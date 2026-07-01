package oracle

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers"
	binanceapi "noah/oracle/providers/api/binance"
	"noah/oracle/providers/base"
	"noah/oracle/providers/base/api"
	basetestutil "noah/oracle/providers/base/testutil"
	"noah/oracle/providers/base/websocket"
	providertypes "noah/oracle/providers/types"
	oracletestutil "noah/oracle/testutil"
	"noah/oracle/types"
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
	aggregator.EXPECT().GetPrices().Return(types.Prices{}).AnyTimes()
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

func TestPlanUpdateAddsNewProvider(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testBinanceAPIProviderConfig(markets)
	oldCfg := testOracleConfig(denoms, map[string]providers.Config{})
	newCfg := testOracleConfig(denoms, map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})
	oracle := &Oracle{logger: log.NewNopLogger()}

	plan, err := oracle.planUpdate(oldCfg, newCfg, nil)

	require.NoError(t, err)
	require.Empty(t, plan.remove)
	require.Empty(t, plan.stop)
	require.Empty(t, plan.updates)
	require.Len(t, plan.set, 1)
	require.Contains(t, plan.set, providerCfg.Name)
	require.Len(t, plan.start, 1)
	require.Same(t, plan.set[providerCfg.Name], plan.start[0])
}

func TestPlanUpdateRemovesDeletedProvider(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	oldCfg := testOracleConfig(denoms, map[string]providers.Config{
		"unknown": providerCfg,
	})
	newCfg := testOracleConfig(denoms, map[string]providers.Config{})
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", markets, denoms)
	oracle := &Oracle{logger: log.NewNopLogger()}

	plan, err := oracle.planUpdate(oldCfg, newCfg, map[string]*base.Provider{
		"unknown": provider.provider,
	})

	require.NoError(t, err)
	require.Equal(t, []string{"unknown"}, plan.remove)
	require.Equal(t, []*base.Provider{provider.provider}, plan.stop)
	require.Empty(t, plan.set)
	require.Empty(t, plan.start)
	require.Empty(t, plan.updates)
}

func TestPlanUpdateReplacesProviderWhenRuntimeConfigChanges(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testBinanceAPIProviderConfig(markets)
	oldCfg := testOracleConfig(denoms, map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})
	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := testOracleConfig(denoms, map[string]providers.Config{
		newProviderCfg.Name: newProviderCfg,
	})
	ctrl := gomock.NewController(t)
	oldProvider := newMockProvider(t, ctrl, providerCfg.Name, markets, denoms)
	oracle := &Oracle{logger: log.NewNopLogger()}

	plan, err := oracle.planUpdate(oldCfg, newCfg, map[string]*base.Provider{
		providerCfg.Name: oldProvider.provider,
	})

	require.NoError(t, err)
	require.Empty(t, plan.remove)
	require.Equal(t, []*base.Provider{oldProvider.provider}, plan.stop)
	require.Len(t, plan.set, 1)
	require.Contains(t, plan.set, providerCfg.Name)
	require.NotSame(t, oldProvider.provider, plan.set[providerCfg.Name])
	require.Len(t, plan.start, 1)
	require.Same(t, plan.set[providerCfg.Name], plan.start[0])
	require.Empty(t, plan.updates)
}

func TestPlanUpdateReturnsErrorWhenRuntimeProviderIsMissing(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(denoms, map[string]providers.Config{
		"unknown": providerCfg,
	})
	oracle := &Oracle{logger: log.NewNopLogger()}

	_, err := oracle.planUpdate(cfg, cfg, map[string]*base.Provider{})

	require.ErrorContains(t, err, `provider "unknown" missing from runtime state`)
}

func TestPlanUpdateReturnsNoopForEquivalentConfig(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(denoms, map[string]providers.Config{
		"unknown": providerCfg,
	})
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", markets, denoms)
	oracle := &Oracle{logger: log.NewNopLogger()}

	plan, err := oracle.planUpdate(cfg, cfg, map[string]*base.Provider{
		"unknown": provider.provider,
	})

	require.NoError(t, err)
	require.Empty(t, plan.remove)
	require.Empty(t, plan.stop)
	require.Empty(t, plan.set)
	require.Empty(t, plan.start)
	require.Empty(t, plan.updates)
}

func TestSameProviderConfigComparesIdentityTypeAndTransportConfig(t *testing.T) {
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}

	testCases := []struct {
		name string
		a    providers.Config
		b    providers.Config
		want bool
	}{
		{
			name: "matching api config",
			a:    testUnknownAPIProviderConfig("unknown", markets),
			b:    testUnknownAPIProviderConfig("unknown", markets),
			want: true,
		},
		{
			name: "different provider name",
			a:    testUnknownAPIProviderConfig("unknown", markets),
			b: func() providers.Config {
				cfg := testUnknownAPIProviderConfig("other", markets)
				return cfg
			}(),
			want: false,
		},
		{
			name: "different provider type",
			a:    testUnknownAPIProviderConfig("unknown", markets),
			b:    testWebSocketProviderConfig("unknown", markets),
			want: false,
		},
		{
			name: "matching websocket config",
			a:    testWebSocketProviderConfig("unknown", markets),
			b:    testWebSocketProviderConfig("unknown", markets),
			want: true,
		},
		{
			name: "different websocket config",
			a:    testWebSocketProviderConfig("unknown", markets),
			b: func() providers.Config {
				cfg := testWebSocketProviderConfig("unknown", markets)
				cfg.WebSocket.ReadTimeout += time.Second
				return cfg
			}(),
			want: false,
		},
		{
			name: "invalid provider type",
			a: providers.Config{
				Name: "unknown",
				Type: base.TransportType("unknown"),
			},
			b: providers.Config{
				Name: "unknown",
				Type: base.TransportType("unknown"),
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sameProviderConfig(tc.a, tc.b))
		})
	}
}

func TestSameWebSocketConfig(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*websocket.Config)
		want   bool
	}{
		{
			name: "equal",
			want: true,
		},
		{
			name: "name differs",
			mutate: func(cfg *websocket.Config) {
				cfg.Name = "other"
			},
		},
		{
			name: "max buffer size differs",
			mutate: func(cfg *websocket.Config) {
				cfg.MaxBufferSize++
			},
		},
		{
			name: "reconnection timeout differs",
			mutate: func(cfg *websocket.Config) {
				cfg.ReconnectionTimeout++
			},
		},
		{
			name: "post connection timeout differs",
			mutate: func(cfg *websocket.Config) {
				cfg.PostConnectionTimeout++
			},
		},
		{
			name: "endpoints differ",
			mutate: func(cfg *websocket.Config) {
				cfg.Endpoints = append(cfg.Endpoints, providertypes.Endpoint{URL: "wss://backup.example.invalid/stream"})
			},
		},
		{
			name: "handshake timeout differs",
			mutate: func(cfg *websocket.Config) {
				cfg.HandshakeTimeout++
			},
		},
		{
			name: "enable compression differs",
			mutate: func(cfg *websocket.Config) {
				cfg.EnableCompression = !cfg.EnableCompression
			},
		},
		{
			name: "read timeout differs",
			mutate: func(cfg *websocket.Config) {
				cfg.ReadTimeout++
			},
		},
		{
			name: "write timeout differs",
			mutate: func(cfg *websocket.Config) {
				cfg.WriteTimeout++
			},
		},
		{
			name: "ping interval differs",
			mutate: func(cfg *websocket.Config) {
				cfg.PingInterval++
			},
		},
		{
			name: "write interval differs",
			mutate: func(cfg *websocket.Config) {
				cfg.WriteInterval++
			},
		},
		{
			name: "max read error count differs",
			mutate: func(cfg *websocket.Config) {
				cfg.MaxReadErrorCount++
			},
		},
		{
			name: "max tickers per connection differs",
			mutate: func(cfg *websocket.Config) {
				cfg.MaxTickersPerConnection++
			},
		},
		{
			name: "max subscriptions per batch differs",
			mutate: func(cfg *websocket.Config) {
				cfg.MaxSubscriptionsPerBatch++
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := testWebSocketConfig("unknown")
			b := testWebSocketConfig("unknown")
			if tc.mutate != nil {
				tc.mutate(&b)
			}

			require.Equal(t, tc.want, sameWebSocketConfig(a, b))
		})
	}
}

func TestSameDenoms(t *testing.T) {
	testCases := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{
			name: "same order",
			a:    []string{"uusd", "ukrw"},
			b:    []string{"uusd", "ukrw"},
			want: true,
		},
		{
			name: "different order",
			a:    []string{"uusd", "ukrw"},
			b:    []string{"ukrw", "uusd"},
			want: true,
		},
		{
			name: "different duplicate count",
			a:    []string{"uusd", "uusd"},
			b:    []string{"uusd", "ukrw"},
			want: false,
		},
		{
			name: "missing denom",
			a:    []string{"uusd", "ukrw"},
			b:    []string{"uusd", "ueur"},
			want: false,
		},
		{
			name: "different length",
			a:    []string{"uusd"},
			b:    []string{"uusd", "ukrw"},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sameDenoms(tc.a, tc.b))
		})
	}
}

func TestSameMarkets(t *testing.T) {
	usdMarket := providertypes.Market{Denom: "uusd", Symbol: "USDTUSD"}
	krwMarket := providertypes.Market{Denom: "ukrw", Symbol: "KRWUSD"}
	eurMarket := providertypes.Market{Denom: "ueur", Symbol: "EURUSD"}

	testCases := []struct {
		name string
		a    providertypes.Markets
		b    providertypes.Markets
		want bool
	}{
		{
			name: "same order",
			a:    providertypes.Markets{usdMarket, krwMarket},
			b:    providertypes.Markets{usdMarket, krwMarket},
			want: true,
		},
		{
			name: "different order",
			a:    providertypes.Markets{usdMarket, krwMarket},
			b:    providertypes.Markets{krwMarket, usdMarket},
			want: true,
		},
		{
			name: "different duplicate count",
			a:    providertypes.Markets{usdMarket, usdMarket},
			b:    providertypes.Markets{usdMarket, krwMarket},
			want: false,
		},
		{
			name: "missing market",
			a:    providertypes.Markets{usdMarket, krwMarket},
			b:    providertypes.Markets{usdMarket, eurMarket},
			want: false,
		},
		{
			name: "different length",
			a:    providertypes.Markets{usdMarket},
			b:    providertypes.Markets{usdMarket, krwMarket},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sameMarkets(tc.a, tc.b))
		})
	}
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

func testMarkets() providertypes.Markets {
	return providertypes.Markets{
		{Denom: "uusd", Symbol: "USDTUSD"},
		{Denom: "ukrw", Symbol: "KRWUSD"},
	}
}

func testBinanceAPIProviderConfig(markets providertypes.Markets) providers.Config {
	cfg := testUnknownAPIProviderConfig(binanceapi.Name, markets)
	cfg.API = binanceapi.DefaultNonUSAPIConfig
	return cfg
}

func testWebSocketProviderConfig(name string, markets providertypes.Markets) providers.Config {
	return providers.Config{
		Name:      name,
		Type:      base.WebSocket,
		Markets:   markets,
		WebSocket: testWebSocketConfig(name),
	}
}

func testWebSocketConfig(name string) websocket.Config {
	return websocket.Config{
		Name:                     name,
		MaxBufferSize:            1,
		ReconnectionTimeout:      time.Second,
		PostConnectionTimeout:    time.Second,
		Endpoints:                []providertypes.Endpoint{{URL: "wss://example.invalid/stream"}},
		HandshakeTimeout:         time.Second,
		EnableCompression:        false,
		ReadTimeout:              time.Second,
		WriteTimeout:             time.Second,
		PingInterval:             time.Second,
		WriteInterval:            time.Second,
		MaxReadErrorCount:        1,
		MaxTickersPerConnection:  1,
		MaxSubscriptionsPerBatch: 1,
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
