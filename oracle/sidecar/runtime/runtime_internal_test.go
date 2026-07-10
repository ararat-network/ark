package runtime

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"ark/oracle/sidecar/chainstate"
	"ark/oracle/sidecar/providers"
	"ark/oracle/sidecar/providers/base"
	"ark/oracle/sidecar/providers/base/api"
	basetestutil "ark/oracle/sidecar/providers/base/testutil"
	providertypes "ark/oracle/sidecar/providers/types"
	"ark/oracle/sidecar/resolver"
	oracletypes "ark/oracle/sidecar/types"
)

func TestNewRuntimeBuildsConfigOwnedProviderSet(t *testing.T) {
	testCases := []struct {
		name        string
		fallback    []string
		wantTickers []providertypes.Ticker
	}{
		{
			name:        "filters markets to fallback denoms",
			fallback:    []string{"uusd"},
			wantTickers: []providertypes.Ticker{"NOAHUSD"},
		},
		{
			name:        "keeps provider without an active market",
			fallback:    []string{"ueur"},
			wantTickers: []providertypes.Ticker{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			fetcher := basetestutil.NewMockFetcher(ctrl)
			fetcher.EXPECT().Name().Return("logger-test").AnyTimes()
			fetcher.EXPECT().Type().Return(base.API).AnyTimes()
			provider, err := base.NewProvider(
				"logger-test",
				base.API,
				providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
				fetcher,
			)
			require.NoError(t, err)

			cfg := testRuntimeLoggerConfig()
			cfg.FallbackDenoms = tc.fallback
			oracle, err := NewRuntime(
				cfg,
				WithProviderFactory(func(
					_ providers.Config,
					markets providertypes.Markets,
				) (*base.Provider, error) {
					provider.UpdateMarkets(markets)
					return provider, nil
				}),
			)
			require.NoError(t, err)

			managed, ok := oracle.providers["logger-test"]
			require.True(t, ok)
			require.Same(t, provider, managed.provider)
			require.Equal(t, tc.wantTickers, provider.GetTickers())
		})
	}
}

func TestGetPriceSnapshotReturnsExactCommittedGeneration(t *testing.T) {
	timestamp := time.Now().UTC()
	oracle := &Runtime{
		denoms: []string{"ukrw"},
		priceSnapshot: oracletypes.PriceSnapshot{
			Prices: oracletypes.DenomPrices{
				"uusd": big.NewFloat(1.25),
			},
			Timestamp: timestamp,
		},
	}

	snapshot := oracle.GetPriceSnapshot()

	require.Equal(t, timestamp, snapshot.Timestamp)
	require.Len(t, snapshot.Prices, 1)
	require.Zero(t, snapshot.Prices["uusd"].Cmp(big.NewFloat(1.25)))
	require.NotContains(t, snapshot.Prices, "ukrw")

	snapshot.Prices["uusd"].SetInt64(99)
	require.Zero(t, oracle.GetPriceSnapshot().Prices["uusd"].Cmp(big.NewFloat(1.25)))
}

func TestRuntimeOwnsConstructionAndUpdateConfigs(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("logger-test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()
	provider, err := base.NewProvider(
		"logger-test",
		base.API,
		providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		fetcher,
	)
	require.NoError(t, err)

	cfg := testRuntimeLoggerConfig()
	cfg.Resolver.Routes = map[string][]resolver.Route{
		"uusd": {{Name: "direct", Pairs: []oracletypes.Pair{"NOAH/USD"}}},
	}
	cfg.Resolver.BootstrapPrices = []resolver.BootstrapPrice{{
		Pair:       "NOAH/USD",
		Price:      "0.25",
		ValidUntil: "2030-01-01T00:00:00Z",
	}}
	oracle, err := NewRuntime(
		cfg,
		WithProviderFactory(func(
			_ providers.Config,
			markets providertypes.Markets,
		) (*base.Provider, error) {
			provider.UpdateMarkets(markets)
			return provider, nil
		}),
	)
	require.NoError(t, err)

	cfg.FallbackDenoms[0] = "ukrw"
	constructionProviderCfg := cfg.Providers["logger-test"]
	constructionProviderCfg.Markets[0].Symbol = "MUTATED"
	cfg.Resolver.Routes["uusd"][0].Pairs[0] = "NOAH/KRW"
	cfg.Resolver.BootstrapPrices[0].Price = "99"
	require.Equal(t, []string{"uusd"}, oracle.denoms)
	require.Equal(t, providertypes.Ticker("NOAHUSD"), oracle.cfg.Providers["logger-test"].Markets[0].Symbol)
	require.Equal(t, oracletypes.Pair("NOAH/USD"), oracle.cfg.Resolver.Routes["uusd"][0].Pairs[0])
	require.Equal(t, "0.25", oracle.cfg.Resolver.BootstrapPrices[0].Price)

	nextCfg := testRuntimeLoggerConfig()
	nextProviderCfg := nextCfg.Providers["logger-test"]
	nextProviderCfg.Markets = providertypes.Markets{{Pair: "NOAH/KRW", Symbol: "NOAHKRW"}}
	nextCfg.Providers["logger-test"] = nextProviderCfg
	nextCfg.FallbackDenoms = []string{"ukrw"}
	nextCfg.Resolver.Routes = map[string][]resolver.Route{
		"ukrw": {{Name: "direct", Pairs: []oracletypes.Pair{"NOAH/KRW"}}},
	}
	nextCfg.Resolver.BootstrapPrices = []resolver.BootstrapPrice{{
		Pair:       "NOAH/KRW",
		Price:      "250",
		ValidUntil: "2030-01-01T00:00:00Z",
	}}
	require.NoError(t, oracle.Update(nextCfg))

	nextCfg.FallbackDenoms[0] = "uusd"
	nextProviderCfg = nextCfg.Providers["logger-test"]
	nextProviderCfg.Markets[0].Symbol = "MUTATED"
	nextCfg.Resolver.Routes["ukrw"][0].Pairs[0] = "NOAH/USD"
	nextCfg.Resolver.BootstrapPrices[0].Price = "99"
	require.Equal(t, []string{"ukrw"}, oracle.denoms)
	require.Equal(t, []providertypes.Ticker{"NOAHKRW"}, provider.GetTickers())
	require.Equal(t, providertypes.Ticker("NOAHKRW"), oracle.cfg.Providers["logger-test"].Markets[0].Symbol)
	require.Equal(t, oracletypes.Pair("NOAH/KRW"), oracle.cfg.Resolver.Routes["ukrw"][0].Pairs[0])
	require.Equal(t, "250", oracle.cfg.Resolver.BootstrapPrices[0].Price)
}

func TestNewRuntimeDerivesComponentLogger(t *testing.T) {
	logs := new(bytes.Buffer)
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().
		Name().
		Return("logger-test").
		AnyTimes()
	fetcher.EXPECT().
		Type().
		Return(base.API).
		AnyTimes()
	provider, err := base.NewProvider(
		"logger-test",
		base.API,
		providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		fetcher,
		base.WithLogger(log.NewNopLogger()),
	)
	require.NoError(t, err)

	oracle, err := NewRuntime(
		testRuntimeLoggerConfig(),
		WithLogger(log.NewLogger(logs)),
		WithProviderFactory(func(
			_ providers.Config,
			markets providertypes.Markets,
		) (*base.Provider, error) {
			provider.UpdateMarkets(markets)
			return provider, nil
		}),
	)
	require.NoError(t, err)

	oracle.logger.Info("runtime component logger")
	output := logs.String()

	require.Contains(t, output, "component=runtime")
	require.NotContains(t, output, "runtime=oracle")
}

func TestNewRuntimePassesLoggerToChainStateClient(t *testing.T) {
	logs := new(bytes.Buffer)
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("logger-test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()
	provider, err := base.NewProvider(
		"logger-test",
		base.API,
		providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		fetcher,
	)
	require.NoError(t, err)

	cfg := testRuntimeLoggerConfig()
	oracle, err := NewRuntime(
		cfg,
		WithLogger(log.NewLogger(logs)),
		WithProviderFactory(func(
			_ providers.Config,
			markets providertypes.Markets,
		) (*base.Provider, error) {
			provider.UpdateMarkets(markets)
			return provider, nil
		}),
	)
	require.NoError(t, err)

	nextCfg := cfg.Clone()
	nextCfg.Client.Interval += time.Second
	require.NoError(t, oracle.Update(nextCfg))
	require.Contains(t, logs.String(), "updated chain state vote-target client config")
}

func testRuntimeLoggerConfig() Config {
	providerCfg := providers.Config{
		Name:          "logger-test",
		TransportType: base.API,
		Markets: providertypes.Markets{
			{Pair: "NOAH/USD", Symbol: "NOAHUSD"},
		},
		MaxPriceAge: time.Minute,
		API: api.Config{
			Name:      "logger-test",
			Timeout:   time.Second,
			Interval:  time.Second,
			Endpoints: []providertypes.Endpoint{{URL: "https://example.invalid/%s/%s"}},
		},
	}

	return Config{
		UpdateInterval: time.Second,
		Providers: map[string]providers.Config{
			providerCfg.Name: providerCfg,
		},
		Client: chainstate.Config{
			Address:  "passthrough:///vote-targets",
			Timeout:  time.Second,
			Interval: time.Second,
		},
		FallbackDenoms: []string{"uusd"},
	}
}
