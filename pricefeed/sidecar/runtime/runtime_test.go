package runtime_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	binanceapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/binance"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/runtime"
	runtimetestutil "github.com/ararat-network/ark/pricefeed/sidecar/runtime/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestNewRuntimeRejectsInvalidInputs(t *testing.T) {
	validCfg := testRuntimeConfigWithUnknownProvider()

	testCases := []struct {
		name    string
		cfg     Config
		opts    []Option
		wantErr string
	}{
		{
			name: "invalid config",
			cfg: func() Config {
				cfg := validCfg
				cfg.UpdateInterval = 0
				return cfg
			}(),
			wantErr: "oracle update interval must be greater than 0",
		},
		{
			name:    "nil logger",
			cfg:     validCfg,
			opts:    []Option{WithLogger(nil)},
			wantErr: "logger is nil",
		},
		{
			name:    "nil provider factory",
			cfg:     validCfg,
			opts:    []Option{WithProviderFactory(nil)},
			wantErr: "provider factory is nil",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRuntime(tc.cfg, tc.opts...)

			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestNewRuntimeWithProviderRegistry(t *testing.T) {
	cfg := testRuntimeConfigWithUnknownProvider()

	t.Run("default registry rejects unregistered provider", func(t *testing.T) {
		_, err := NewRuntime(cfg)

		require.ErrorContains(t, err, "unrecognised provider name: unknown")
	})

	t.Run("registered provider builds", func(t *testing.T) {
		registry := providers.NewRegistry()
		require.NoError(t, registry.RegisterAPI("unknown", func(providers.Config, log.Logger) (api.DataHandler, error) {
			return binanceapi.NewHandler(), nil
		}))

		oracle, err := NewRuntime(cfg, WithProviderRegistry(registry))

		require.NoError(t, err)
		require.NotNil(t, oracle)
	})

	t.Run("nil registry", func(t *testing.T) {
		_, err := NewRuntime(cfg, WithProviderRegistry(nil))

		require.ErrorContains(t, err, "provider registry is nil")
	})
}

func TestConfigValidateRejectsNonCanonicalFallbackDenom(t *testing.T) {
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.FallbackFeeds = []string{"aUSD"}

	err := cfg.Validate()

	require.ErrorContains(t, err, "Ark-native base denom matching")
}

func TestConfigValidateRejectsTooManyFallbackFeeds(t *testing.T) {
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.FallbackFeeds = make([]string, oracletypes.MaxFeeds+1)

	err := cfg.Validate()

	require.ErrorContains(t, err, "fallback feed count")
	require.ErrorContains(t, err, "exceeds maximum")
}

func TestGetPriceSnapshotReturnsCommittedDenoms(t *testing.T) {
	testCases := []struct {
		name              string
		setup             func(t *testing.T) (*Runtime, func())
		wantTimestampZero bool
		wantPrices        map[string]*big.Float
		wantAbsent        []string
	}{
		{
			name: "empty before first tick",
			setup: func(t *testing.T) (*Runtime, func()) {
				t.Helper()

				ctrl := gomock.NewController(t)
				mp := newMockProvider(t, ctrl, "unknown", testMarkets())
				oracle, err := NewRuntime(
					testRuntimeConfigWithUnknownProvider(),
					withInitialProviders(mp.provider),
				)
				require.NoError(t, err)

				return oracle, func() {}
			},
			wantTimestampZero: true,
			wantPrices:        map[string]*big.Float{},
		},
		{
			name: "returns only resolved active feeds",
			setup: func(t *testing.T) (*Runtime, func()) {
				t.Helper()

				ctrl := gomock.NewController(t)
				started := make(chan struct{})
				mp := newMockProvider(t, ctrl, "unknown", testMarkets())
				mp.fetcher.EXPECT().
					Run(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(
						ctx context.Context,
						_ []providertypes.Ticker,
						responseCh chan<- providertypes.Response,
					) error {
						close(started)
						responseCh <- providertypes.NewResponse(
							map[providertypes.Ticker]providertypes.Result{
								"NOAHUSD": providertypes.NewResult(big.NewFloat(0.25), time.Now().UTC()),
							},
							nil,
						)
						<-ctx.Done()
						return ctx.Err()
					})

				feedsClient := runtimetestutil.NewMockChainStateClient(ctrl)
				expectFeedsLifecycle(feedsClient)
				feedsClient.EXPECT().
					Feeds().
					Return([]string{"ausd", "akrw"}, nil).
					AnyTimes()

				cfg := testRuntimeConfigWithUnknownProvider()
				cfg.UpdateInterval = 5 * time.Millisecond
				oracle, err := NewRuntime(
					cfg,
					withInitialProviders(mp.provider),
					WithChainStateClient(feedsClient),
				)
				require.NoError(t, err)

				errCh, cancel := startOracle(t, oracle)
				requireProviderStarted(t, started)
				// A tick can commit before the provider's response lands; wait for the price.
				require.Eventually(t, func() bool {
					return len(oracle.GetPriceSnapshot().Prices) > 0
				}, time.Second, time.Millisecond)

				return oracle, func() {
					cancel()
					requireOracleStopped(t, errCh)
				}
			},
			wantPrices: map[string]*big.Float{
				"ausd": big.NewFloat(4),
			},
			wantAbsent: []string{"akrw", "aeur"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			oracle, cleanup := tc.setup(t)
			defer cleanup()

			snapshot := oracle.GetPriceSnapshot()

			require.Equal(t, tc.wantTimestampZero, snapshot.Timestamp.IsZero())
			require.Len(t, snapshot.Prices, len(tc.wantPrices))
			for denom, wantPrice := range tc.wantPrices {
				gotPrice, ok := snapshot.Prices[denom]
				require.True(t, ok, "missing denom %s", denom)
				require.Zero(t, gotPrice.Cmp(wantPrice), "price mismatch for denom %s", denom)
			}
			for _, denom := range tc.wantAbsent {
				require.NotContains(t, snapshot.Prices, denom)
			}
		})
	}
}
