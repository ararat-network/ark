package runtime_test

import (
	"math/big"
	. "noah/oracle/sidecar/runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"noah/oracle/sidecar/providers"
	oracletestutil "noah/oracle/sidecar/runtime/testutil"
	"noah/oracle/sidecar/types"
)

func TestNewRuntimeRejectsInvalidInputs(t *testing.T) {
	validCfg := testOracleConfig(map[string]providers.Config{})

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
	providers := oracle.GetProviders()
	require.Contains(t, providers, providerCfg.Name)
	require.Equal(t, providerCfg.Name, providers[providerCfg.Name].Name())
}

func TestNewRuntimeSkipsProviderWithoutActiveMarkets(t *testing.T) {
	markets := testMarkets()
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})
	cfg.Resolver = testResolverConfig("ueur", "ark-eur", "ARK/EUR")

	oracle, err := NewRuntime(cfg)

	require.NoError(t, err)
	require.NotContains(t, oracle.GetProviders(), providerCfg.Name)
}

func TestGetProvidersReturnsMapSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	oracle, err := NewRuntime(
		testOracleConfig(map[string]providers.Config{}),
		WithProviders(provider.provider),
		WithResolver(oracletestutil.NewMockPriceResolver(ctrl)),
	)
	require.NoError(t, err)

	providers := oracle.GetProviders()
	delete(providers, "unknown")

	require.Contains(t, oracle.GetProviders(), "unknown")
}

func TestGetPricesReturnsResolverPricesByDenom(t *testing.T) {
	prices := types.Prices{
		"USDT/USD": big.NewFloat(1.23),
	}
	resolver := oracletestutil.NewMockPriceResolver(gomock.NewController(t))
	resolver.EXPECT().GetPrices().Return(prices)
	cfg := testOracleConfig(map[string]providers.Config{})
	cfg.FallbackDenoms = []string{"uusd"}
	oracle, err := NewRuntime(cfg, WithResolver(resolver))
	require.NoError(t, err)

	require.Equal(t, types.DenomPrices{"uusd": big.NewFloat(1.23)}, oracle.GetPrices())
}

func TestGetPricesAppliesConfiguredAbstainDenomOverrides(t *testing.T) {
	prices := types.Prices{
		"USDT/USD": big.NewFloat(1.23),
		"USDT/KRW": big.NewFloat(1300),
	}
	resolver := oracletestutil.NewMockPriceResolver(gomock.NewController(t))
	resolver.EXPECT().GetPrices().Return(prices)
	cfg := testOracleConfig(map[string]providers.Config{})
	cfg.FallbackDenoms = []string{"uusd", "ukrw"}
	cfg.AbstainDenoms = []string{"ukrw", "ujpy"}
	oracle, err := NewRuntime(cfg, WithResolver(resolver))
	require.NoError(t, err)

	got := oracle.GetPrices()

	require.Len(t, got, 2)
	require.Zero(t, got["uusd"].Cmp(big.NewFloat(1.23)))
	require.Zero(t, got["ukrw"].Sign())
	require.NotContains(t, got, "ujpy")
}
