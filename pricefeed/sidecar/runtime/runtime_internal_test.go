package runtime

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"ark/pricefeed/sidecar/chainstate"
	"ark/pricefeed/sidecar/providers"
	"ark/pricefeed/sidecar/providers/base"
	"ark/pricefeed/sidecar/providers/base/api"
	basetestutil "ark/pricefeed/sidecar/providers/base/testutil"
	providertypes "ark/pricefeed/sidecar/providers/types"
	"ark/pricefeed/sidecar/resolver"
	oracletypes "ark/pricefeed/sidecar/types"
)

func TestNewRuntimeBuildsConfigOwnedProviderSet(t *testing.T) {
	testCases := []struct {
		name        string
		fallback    []string
		wantTickers []providertypes.Ticker
	}{
		{
			name:        "filters markets to fallback feeds",
			fallback:    []string{"ausd"},
			wantTickers: []providertypes.Ticker{"NOAHUSD"},
		},
		{
			name:        "keeps provider without an active market",
			fallback:    []string{"aeur"},
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
			cfg.FallbackFeeds = tc.fallback
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
		feeds: []string{"akrw"},
		priceSnapshot: oracletypes.PriceSnapshot{
			Prices: oracletypes.FeedPrices{
				"ausd": big.NewFloat(1.25),
			},
			Timestamp: timestamp,
		},
	}

	snapshot := oracle.GetPriceSnapshot()

	require.Equal(t, timestamp, snapshot.Timestamp)
	require.Len(t, snapshot.Prices, 1)
	require.Zero(t, snapshot.Prices["ausd"].Cmp(big.NewFloat(1.25)))
	require.NotContains(t, snapshot.Prices, "akrw")

	snapshot.Prices["ausd"].SetInt64(99)
	require.Zero(t, oracle.GetPriceSnapshot().Prices["ausd"].Cmp(big.NewFloat(1.25)))
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
		"ausd": {{Name: "direct", Pairs: []oracletypes.Pair{"NOAH/USD"}}},
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

	cfg.FallbackFeeds[0] = "akrw"
	constructionProviderCfg := cfg.Providers["logger-test"]
	constructionProviderCfg.Markets[0].Symbol = "MUTATED"
	cfg.Resolver.Routes["ausd"][0].Pairs[0] = "NOAH/KRW"
	cfg.Resolver.BootstrapPrices[0].Price = "99"
	require.Equal(t, []string{"ausd"}, oracle.feeds)
	require.Equal(t, providertypes.Ticker("NOAHUSD"), oracle.cfg.Providers["logger-test"].Markets[0].Symbol)
	require.Equal(t, oracletypes.Pair("NOAH/USD"), oracle.cfg.Resolver.Routes["ausd"][0].Pairs[0])
	require.Equal(t, "0.25", oracle.cfg.Resolver.BootstrapPrices[0].Price)

	nextCfg := testRuntimeLoggerConfig()
	nextProviderCfg := nextCfg.Providers["logger-test"]
	nextProviderCfg.Markets = providertypes.Markets{{Pair: "NOAH/KRW", Symbol: "NOAHKRW"}}
	nextCfg.Providers["logger-test"] = nextProviderCfg
	nextCfg.FallbackFeeds = []string{"akrw"}
	nextCfg.Resolver.Routes = map[string][]resolver.Route{
		"akrw": {{Name: "direct", Pairs: []oracletypes.Pair{"NOAH/KRW"}}},
	}
	nextCfg.Resolver.BootstrapPrices = []resolver.BootstrapPrice{{
		Pair:       "NOAH/KRW",
		Price:      "250",
		ValidUntil: "2030-01-01T00:00:00Z",
	}}
	require.NoError(t, oracle.Update(nextCfg))

	nextCfg.FallbackFeeds[0] = "ausd"
	nextProviderCfg = nextCfg.Providers["logger-test"]
	nextProviderCfg.Markets[0].Symbol = "MUTATED"
	nextCfg.Resolver.Routes["akrw"][0].Pairs[0] = "NOAH/USD"
	nextCfg.Resolver.BootstrapPrices[0].Price = "99"
	require.Equal(t, []string{"akrw"}, oracle.feeds)
	require.Equal(t, []providertypes.Ticker{"NOAHKRW"}, provider.GetTickers())
	require.Equal(t, providertypes.Ticker("NOAHKRW"), oracle.cfg.Providers["logger-test"].Markets[0].Symbol)
	require.Equal(t, oracletypes.Pair("NOAH/KRW"), oracle.cfg.Resolver.Routes["akrw"][0].Pairs[0])
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
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
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
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
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
	require.Contains(t, logs.String(), "updated chain state feed client config")
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
			Address:  "passthrough:///feeds",
			Timeout:  time.Second,
			Interval: time.Second,
		},
		FallbackFeeds: []string{"ausd"},
	}
}
