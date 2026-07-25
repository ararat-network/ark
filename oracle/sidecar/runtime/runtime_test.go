package runtime_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	providertypes "ark/oracle/sidecar/providers/types"
	. "ark/oracle/sidecar/runtime"
	oracletestutil "ark/oracle/sidecar/runtime/testutil"
	oracletypes "ark/x/oracle/types"
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

func TestConfigValidateRejectsNonCanonicalFallbackDenom(t *testing.T) {
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.FallbackDenoms = []string{"aUSD"}

	err := cfg.Validate()

	require.ErrorContains(t, err, "canonical lowercase Ark-native base denom")
}

func TestConfigValidateRejectsTooManyFallbackDenoms(t *testing.T) {
	cfg := testRuntimeConfigWithUnknownProvider()
	cfg.FallbackDenoms = make([]string, oracletypes.MaxVoteTargets+1)

	err := cfg.Validate()

	require.ErrorContains(t, err, "fallback denom count")
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
			name: "returns only resolved active denoms",
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
								"NOAHUSD": providertypes.NewResult(big.NewFloat(1.25), time.Now().UTC()),
							},
							nil,
						)
						<-ctx.Done()
						return ctx.Err()
					})

				voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
				expectVoteTargetsLifecycle(voteTargetsClient)
				voteTargetsClient.EXPECT().
					VoteTargets().
					Return([]string{"ausd", "akrw"}, nil).
					AnyTimes()

				cfg := testRuntimeConfigWithUnknownProvider()
				cfg.UpdateInterval = 5 * time.Millisecond
				oracle, err := NewRuntime(
					cfg,
					withInitialProviders(mp.provider),
					WithChainStateClient(voteTargetsClient),
				)
				require.NoError(t, err)

				errCh, cancel := startOracle(t, oracle)
				requireProviderStarted(t, started)
				require.Eventually(t, func() bool {
					return !oracle.GetPriceSnapshot().Timestamp.IsZero()
				}, time.Second, time.Millisecond)

				return oracle, func() {
					cancel()
					requireOracleStopped(t, errCh)
				}
			},
			wantPrices: map[string]*big.Float{
				"ausd": big.NewFloat(1.25),
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
