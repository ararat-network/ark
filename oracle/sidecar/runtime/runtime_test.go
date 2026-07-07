package runtime_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"noah/oracle/sidecar/providers"
	providertypes "noah/oracle/sidecar/providers/types"
	. "noah/oracle/sidecar/runtime"
	oracletestutil "noah/oracle/sidecar/runtime/testutil"
	oracletypes "noah/oracle/sidecar/types"
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRuntime(tc.cfg, tc.opts...)

			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestNewRuntimeBuildsConfiguredProviders(t *testing.T) {
	markets := testMarkets()
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	oracle, err := NewRuntime(cfg)

	require.NoError(t, err)
	providers := GetProvidersForTest(oracle)
	require.Contains(t, providers, providerCfg.Name)
	require.Equal(t, providerCfg.Name, providers[providerCfg.Name].Name())
}

func TestNewRuntimeFiltersProviderMarketsToFallbackDenoms(t *testing.T) {
	markets := providertypes.Markets{
		{Pair: "ARK/USD", Symbol: "ARKUSD"},
		{Pair: "ARK/KRW", Symbol: "ARKKRW"},
	}
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})
	cfg.FallbackDenoms = []string{"uusd"}

	oracle, err := NewRuntime(cfg)

	require.NoError(t, err)
	require.Equal(t, []providertypes.Ticker{"ARKUSD"}, GetProvidersForTest(oracle)[providerCfg.Name].GetTickers())
}

func TestNewRuntimeKeepsConfiguredProvidersWithoutActiveFallbackMarkets(t *testing.T) {
	markets := testMarkets()
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})
	cfg.Resolver = testResolverConfig("ueur", "ark-eur", "ARK/EUR")
	cfg.FallbackDenoms = []string{"ueur"}

	oracle, err := NewRuntime(cfg)

	require.NoError(t, err)
	providers := GetProvidersForTest(oracle)
	require.Contains(t, providers, providerCfg.Name)
	require.Empty(t, providers[providerCfg.Name].GetTickers())
}

func TestProviderSnapshotForTestReturnsMapSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	oracle, err := NewRuntime(
		testRuntimeConfigWithUnknownProvider(),
		WithProviders(provider.provider),
		WithResolver(oracletestutil.NewMockPriceResolver(ctrl)),
	)
	require.NoError(t, err)

	providers := GetProvidersForTest(oracle)
	delete(providers, "unknown")

	require.Contains(t, GetProvidersForTest(oracle), "unknown")
}

func TestGetPriceSnapshotProjectsActiveDenoms(t *testing.T) {
	testCases := []struct {
		name              string
		setup             func(t *testing.T) (*Runtime, func())
		wantTimestampZero bool
		wantPrices        map[string]*big.Float
		wantAbsent        []string
	}{
		{
			name: "zero fills active denoms before first tick",
			setup: func(t *testing.T) (*Runtime, func()) {
				t.Helper()

				ctrl := gomock.NewController(t)
				provider := newMockProvider(t, ctrl, "unknown", testMarkets())
				oracle, err := NewRuntime(
					testRuntimeConfigWithUnknownProvider(),
					WithProviders(provider.provider),
					WithResolver(oracletestutil.NewMockPriceResolver(ctrl)),
				)
				require.NoError(t, err)

				return oracle, func() {}
			},
			wantTimestampZero: true,
			wantPrices: map[string]*big.Float{
				"uusd": new(big.Float),
				"ukrw": new(big.Float),
			},
		},
		{
			name: "filters committed snapshot to active denoms",
			setup: func(t *testing.T) (*Runtime, func()) {
				t.Helper()

				ctrl := gomock.NewController(t)
				started := make(chan struct{})
				provider := newMockProvider(t, ctrl, "unknown", testMarkets())
				expectFetcherRunAnyTimes(provider.fetcher, started)

				voteTargetsClient := oracletestutil.NewMockChainStateClient(ctrl)
				expectVoteTargetsLifecycle(voteTargetsClient)
				voteTargetsClient.EXPECT().
					VoteTargets().
					Return([]string{"uusd", "ukrw"}, nil).
					AnyTimes()

				cfg := testRuntimeConfigWithUnknownProvider()
				cfg.UpdateInterval = 5 * time.Millisecond
				oracle, err := NewRuntime(
					cfg,
					WithProviders(provider.provider),
					WithResolver(newRecordingResolver(oracletypes.Prices{
						"ARK/USD": big.NewFloat(1.25),
						"ARK/EUR": big.NewFloat(0.90),
					})),
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
				"uusd": big.NewFloat(1.25),
				"ukrw": new(big.Float),
			},
			wantAbsent: []string{"ueur"},
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
