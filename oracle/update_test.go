package oracle

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers"
	"noah/oracle/providers/base"
	"noah/oracle/providers/base/api"
	basetestutil "noah/oracle/providers/base/testutil"
	providertypes "noah/oracle/providers/types"
	oracletestutil "noah/oracle/testutil"
)

func TestUpdateOracleDoesNotStopExistingProviderWhenReplacementBuildFails(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(denoms, map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", markets, denoms)
	expectFetcherRun(provider.fetcher, started)

	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.provider.Start(context.Background())
	}()
	requireProviderStarted(t, started)

	oracle, err := NewOracle(
		cfg,
		oracletestutil.NewMockPriceAggregator(ctrl),
		WithProviders(provider.provider),
	)
	require.NoError(t, err)

	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := testOracleConfig(denoms, map[string]providers.Config{
		"unknown": newProviderCfg,
	})

	err = oracle.UpdateOracle(newCfg)
	require.ErrorContains(t, err, "unrecognised provider name")
	require.True(t, provider.provider.IsRunning())
	require.Same(t, provider.provider, oracle.providers["unknown"])
	require.Equal(t, cfg, oracle.cfg)

	provider.provider.Stop()
	requireProviderStopped(t, errCh)
}

func TestUpdateOracleInjectsMarketOnlyChangeWithoutRebuildingProvider(t *testing.T) {
	denoms := []string{"uusd"}
	oldMarkets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	newMarkets := providertypes.Markets{{Denom: "uusd", Symbol: "BUSDUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", oldMarkets)
	cfg := testOracleConfig(denoms, map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", oldMarkets, denoms)

	oracle, err := NewOracle(
		cfg,
		oracletestutil.NewMockPriceAggregator(ctrl),
		WithProviders(provider.provider),
	)
	require.NoError(t, err)

	newProviderCfg := providerCfg
	newProviderCfg.Markets = newMarkets
	newCfg := testOracleConfig(denoms, map[string]providers.Config{
		"unknown": newProviderCfg,
	})

	require.NoError(t, oracle.UpdateOracle(newCfg))
	require.Same(t, provider.provider, oracle.providers["unknown"])
	require.Equal(t, newCfg, oracle.cfg)
}

func TestUpdateOracleAppliesUpdateIntervalWithoutRestart(t *testing.T) {
	cfg := testOracleConfig([]string{"uusd"}, map[string]providers.Config{})
	cfg.UpdateInterval = time.Hour
	ctrl := gomock.NewController(t)
	aggregator := oracletestutil.NewMockPriceAggregator(ctrl)
	aggregateCh := make(chan struct{}, 1)
	aggregator.EXPECT().Reset().AnyTimes()
	aggregator.EXPECT().
		AggregatePrices().
		Do(func() {
			select {
			case aggregateCh <- struct{}{}:
			default:
			}
		}).
		AnyTimes()

	oracle, err := NewOracle(cfg, aggregator)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Start(ctx)
	}()
	requireOracleStarted(t, oracle)

	newCfg := cfg
	newCfg.UpdateInterval = 10 * time.Millisecond
	require.NoError(t, oracle.UpdateOracle(newCfg))

	select {
	case <-aggregateCh:
	case <-time.After(time.Second):
		t.Fatal("oracle did not apply updated interval")
	}

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartProviderDoesNotMarkIntentionalProviderStopAsFailed(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", markets, denoms)
	expectFetcherRun(provider.fetcher, started)

	oracle := &Oracle{
		logger: log.NewNopLogger(),
	}
	mainCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oracle.startProvider(mainCtx, provider.provider)
	requireProviderStarted(t, started)

	provider.provider.Stop()
	requireWaitGroupDone(t, &oracle.wg)
}

type mockProvider struct {
	provider *base.Provider
	fetcher  *basetestutil.MockFetcher
}

func newMockProvider(
	t *testing.T,
	ctrl *gomock.Controller,
	name string,
	markets providertypes.Markets,
	denoms []string,
) mockProvider {
	t.Helper()

	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, name, base.API)

	provider, err := base.NewProvider(
		name,
		base.API,
		markets,
		fetcher,
		base.WithDenoms(denoms),
	)
	require.NoError(t, err)

	return mockProvider{
		provider: provider,
		fetcher:  fetcher,
	}
}

func expectFetcher(fetcher *basetestutil.MockFetcher, name string, providerType base.TransportType) {
	fetcher.EXPECT().
		Name().
		Return(name).
		AnyTimes()
	fetcher.EXPECT().
		Type().
		Return(providerType).
		AnyTimes()
	fetcher.EXPECT().
		ResponseBufferSize(gomock.Any()).
		Return(1).
		AnyTimes()
}

func expectFetcherRun(fetcher *basetestutil.MockFetcher, started chan<- struct{}) {
	fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, _ chan<- providertypes.Response) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
}

func testUnknownAPIProviderConfig(name string, markets providertypes.Markets) providers.Config {
	return providers.Config{
		Name:    name,
		Type:    base.API,
		Markets: markets,
		API: api.Config{
			Name:      name,
			Interval:  time.Second,
			Endpoints: []providertypes.Endpoint{{URL: "https://example.invalid/prices"}},
		},
	}
}

func testOracleConfig(denoms []string, providerCfgs map[string]providers.Config) Config {
	return Config{
		UpdateInterval: time.Second,
		MaxPriceAge:    time.Minute,
		Providers:      providerCfgs,
		Host:           "127.0.0.1",
		Port:           "0",
		Denoms:         denoms,
	}
}

func requireProviderStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
}

func requireProviderStopped(t *testing.T, errCh <-chan error) {
	t.Helper()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("provider did not stop")
	}
}

func requireOracleStarted(t *testing.T, oracle *Oracle) {
	t.Helper()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatal("oracle did not start")
		case <-ticker.C:
			if !oracle.IsRunning() {
				continue
			}
			mainCtx, _ := oracle.getMainCtx()
			if mainCtx != nil {
				return
			}
		}
	}
}

func requireOracleStopped(t *testing.T, errCh <-chan error) {
	t.Helper()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop")
	}
}

func requireWaitGroupDone(t *testing.T, wg interface{ Wait() }) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wait group did not finish")
	}
}
