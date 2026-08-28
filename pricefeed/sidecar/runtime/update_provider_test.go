package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	. "github.com/ararat-network/ark/pricefeed/sidecar/runtime"
)

func TestUpdateConfigDoesNotStopExistingProviderWhenReplacementBuildFails(t *testing.T) {
	markets := providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	stopped := make(chan struct{})
	mp := newMockProvider(t, ctrl, "unknown", markets)
	mp.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, _ chan<- providertypes.Response) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			return ctx.Err()
		})

	oracle, err := NewRuntime(
		cfg,
		WithChainStateClient(newPassthroughChainStateClient(t, ctrl)),
		withInitialProviders(mp.provider),
	)
	require.NoError(t, err)
	errCh, cancel := startOracle(t, oracle)
	requireProviderStarted(t, started)

	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := testOracleConfig(map[string]providers.Config{
		"unknown": newProviderCfg,
	})

	err = oracle.Update(newCfg)
	require.ErrorContains(t, err, "unrecognised provider name")

	select {
	case <-stopped:
		t.Fatal("provider stopped after rejected config update")
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	requireOracleStopped(t, errCh)
	requireSignal(t, stopped, "provider did not stop with runtime")
}

func TestUpdateConfigDoesNotMutateRuntimeStateWhenProviderPlanFails(t *testing.T) {
	markets := testRouteMarkets()
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", markets)
	client, clientRecorder := newRecordingChainStateClient(t, ctrl)
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		WithChainStateClient(client),
	)
	require.NoError(t, err)

	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := cfg
	newCfg.Providers = map[string]providers.Config{
		"unknown": newProviderCfg,
	}
	newCfg.Resolver = testResolverConfig("akrw", "noah-krw", "NOAH/USD", "USD/KRW")
	newCfg.Client.Interval = 10 * time.Millisecond
	newCfg.FallbackFeeds = []string{"ausd"}

	err = oracle.Update(newCfg)

	require.ErrorContains(t, err, "unrecognised provider name")
	require.Empty(t, clientRecorder.updateConfigs())
}

func TestUpdateConfigInjectsMarketOnlyChangeWithoutRebuildingProvider(t *testing.T) {
	oldMarkets := providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}}
	newMarkets := providertypes.Markets{{Pair: "NOAH/USD", Symbol: "BUSDUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", oldMarkets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", oldMarkets)

	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
	)
	require.NoError(t, err)

	newProviderCfg := providerCfg
	newProviderCfg.Markets = newMarkets
	newCfg := testOracleConfig(map[string]providers.Config{
		"unknown": newProviderCfg,
	})

	require.NoError(t, oracle.Update(newCfg))
	require.Equal(t, newMarkets.Tickers(), mp.provider.GetTickers())
}

func TestUpdateConfigAppliesMaxPriceAgeWithoutRebuildingProvider(t *testing.T) {
	providerCfg := testUnknownAPIProviderConfig("unknown", testMarkets())
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, providerCfg.Name, providerCfg.Markets)
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
	)
	require.NoError(t, err)

	providerCfg.MaxPriceAge = 2 * time.Minute
	newCfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	require.NoError(t, oracle.Update(newCfg))
}

func TestUpdateConfigAppliesBootstrapPriceWithoutRebuildingProvider(t *testing.T) {
	providerCfg := testUnknownAPIProviderConfig("unknown", testMarkets())
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, providerCfg.Name, providerCfg.Markets)
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
	)
	require.NoError(t, err)

	newCfg := cfg
	newCfg.Resolver.BootstrapPrices = []resolver.BootstrapPrice{{
		Pair:       "NOAH/USD",
		Price:      "0.25",
		ValidUntil: "2030-01-01T00:00:00Z",
	}}

	require.NoError(t, oracle.Update(newCfg))
}

func TestUpdateConfigRestartsStoppedProviderOnMarketOnlyChange(t *testing.T) {
	oldMarkets := providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}}
	newMarkets := providertypes.Markets{{Pair: "NOAH/USD", Symbol: "BUSDUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", oldMarkets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", oldMarkets)
	firstStarted := make(chan struct{})
	restarted := make(chan struct{})
	expectFetcherRunErrorThenBlock(
		t,
		mp.fetcher,
		firstStarted,
		restarted,
		oldMarkets.Tickers(),
		newMarkets.Tickers(),
	)
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
		WithChainStateClient(newPassthroughChainStateClient(t, ctrl)),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, firstStarted)

	newProviderCfg := providerCfg
	newProviderCfg.Markets = newMarkets
	newCfg := testOracleConfig(map[string]providers.Config{
		"unknown": newProviderCfg,
	})
	require.NoError(t, oracle.Update(newCfg))

	requireProviderStarted(t, restarted)
	require.Equal(t, newMarkets.Tickers(), mp.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}
func TestUpdateConfigKeepsProviderWhenFallbackFeedsDeactivateMarkets(t *testing.T) {
	providerCfg := testUnknownAPIProviderConfig("unknown", testMarkets())
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	mp := newMockProvider(t, ctrl, "unknown", testMarkets())
	oracle, err := NewRuntime(
		cfg,
		withInitialProviders(mp.provider),
	)
	require.NoError(t, err)

	newCfg := cfg
	newCfg.FallbackFeeds = []string{"aeur"}

	require.NoError(t, oracle.Update(newCfg))
	require.Empty(t, mp.provider.GetTickers())
}
