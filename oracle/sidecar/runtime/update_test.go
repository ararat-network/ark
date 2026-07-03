package runtime_test

import (
	"context"
	"errors"
	. "noah/oracle/sidecar/runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"noah/oracle/sidecar/providers"
	binanceapi "noah/oracle/sidecar/providers/api/binance"
	"noah/oracle/sidecar/providers/base"
	"noah/oracle/sidecar/providers/base/api"
	basetestutil "noah/oracle/sidecar/providers/base/testutil"
	"noah/oracle/sidecar/providers/base/websocket"
	providertypes "noah/oracle/sidecar/providers/types"
	resolverpkg "noah/oracle/sidecar/resolver"
	oracletestutil "noah/oracle/sidecar/runtime/testutil"
	"noah/oracle/sidecar/types"
)

func TestUpdateConfigDoesNotStopExistingProviderWhenReplacementBuildFails(t *testing.T) {
	markets := providertypes.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", markets)
	expectFetcherRun(provider.fetcher, started)

	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.provider.Start(context.Background())
	}()
	requireProviderStarted(t, started)

	oracle, err := NewRuntime(
		cfg,
		WithResolver(oracletestutil.NewMockPriceResolver(ctrl)),
		WithProviders(provider.provider),
	)
	require.NoError(t, err)

	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := testOracleConfig(map[string]providers.Config{
		"unknown": newProviderCfg,
	})

	err = oracle.UpdateConfig(newCfg)
	require.ErrorContains(t, err, "unrecognised provider name")
	require.True(t, provider.provider.IsRunning())
	require.Same(t, provider.provider, oracle.GetProviders()["unknown"])

	provider.provider.Stop()
	requireProviderStopped(t, errCh)
}

func TestUpdateConfigInjectsMarketOnlyChangeWithoutRebuildingProvider(t *testing.T) {
	oldMarkets := providertypes.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}
	newMarkets := providertypes.Markets{{Pair: "USDT/USD", Symbol: "BUSDUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", oldMarkets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", oldMarkets)

	oracle, err := NewRuntime(
		cfg,
		WithResolver(oracletestutil.NewMockPriceResolver(ctrl)),
		WithProviders(provider.provider),
	)
	require.NoError(t, err)

	newProviderCfg := providerCfg
	newProviderCfg.Markets = newMarkets
	newCfg := testOracleConfig(map[string]providers.Config{
		"unknown": newProviderCfg,
	})

	require.NoError(t, oracle.UpdateConfig(newCfg))
	require.Same(t, provider.provider, oracle.GetProviders()["unknown"])
	require.Equal(t, newMarkets.Tickers(), provider.provider.GetTickers())
}

func TestUpdateConfigAppliesUpdateIntervalWithoutRestart(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{})
	cfg.UpdateInterval = time.Hour
	ctrl := gomock.NewController(t)
	resolver := oracletestutil.NewMockPriceResolver(ctrl)
	aggregateCh := make(chan struct{}, 1)
	resolver.EXPECT().Reset().AnyTimes()
	resolver.EXPECT().GetPrices().Return(types.Prices{}).AnyTimes()
	resolver.EXPECT().
		ResolvePrices(gomock.Any()).
		Do(func([]string) {
			select {
			case aggregateCh <- struct{}{}:
			default:
			}
		}).
		AnyTimes()

	oracle, err := NewRuntime(cfg, WithResolver(resolver))
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
	require.NoError(t, oracle.UpdateConfig(newCfg))

	select {
	case <-aggregateCh:
	case <-time.After(time.Second):
		t.Fatal("oracle did not apply updated interval")
	}

	cancel()
	requireOracleStopped(t, errCh)
}

func TestUpdateConfigUpdatesResolverConfig(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{})
	newCfg := cfg
	newCfg.Resolver = testResolverConfig("ukrw", "ark-krw", "ARK/USD", "USD/KRW")

	ctrl := gomock.NewController(t)
	resolver := oracletestutil.NewMockPriceResolver(ctrl)
	resolver.EXPECT().UpdateConfig(newCfg.Resolver)
	oracle, err := NewRuntime(cfg, WithResolver(resolver))
	require.NoError(t, err)

	require.NoError(t, oracle.UpdateConfig(newCfg))
}

func TestUpdateConfigReturnsResolverUpdateErrorWithoutChangingConfig(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{})
	newCfg := cfg
	newCfg.Resolver = testResolverConfig("ukrw", "ark-krw", "ARK/USD", "USD/KRW")
	updateErr := errors.New("resolver update failed")

	ctrl := gomock.NewController(t)
	resolver := oracletestutil.NewMockPriceResolver(ctrl)
	resolver.EXPECT().UpdateConfig(newCfg.Resolver).Return(updateErr)
	oracle, err := NewRuntime(cfg, WithResolver(resolver))
	require.NoError(t, err)

	err = oracle.UpdateConfig(newCfg)

	require.ErrorIs(t, err, updateErr)
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
) mockProvider {
	t.Helper()

	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, name, base.API)

	provider, err := base.NewProvider(
		name,
		base.API,
		markets,
		fetcher,
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
		Name:          name,
		TransportType: base.API,
		Markets:       markets,
		API: api.Config{
			Name:      name,
			Interval:  time.Second,
			Endpoints: []providertypes.Endpoint{{URL: "https://example.invalid/prices"}},
		},
	}
}

func testMarkets() providertypes.Markets {
	return providertypes.Markets{
		{Pair: "USDT/USD", Symbol: "USDTUSD"},
		{Pair: "USDT/KRW", Symbol: "KRWUSD"},
	}
}

func testBinanceAPIProviderConfig(markets providertypes.Markets) providers.Config {
	cfg := testUnknownAPIProviderConfig(binanceapi.Name, markets)
	cfg.API = binanceapi.DefaultNonUSAPIConfig
	return cfg
}

func testWebSocketProviderConfig(name string, markets providertypes.Markets) providers.Config {
	return providers.Config{
		Name:          name,
		TransportType: base.WebSocket,
		Markets:       markets,
		WebSocket:     testWebSocketConfig(name),
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

func testOracleConfig(providerCfgs map[string]providers.Config) Config {
	return Config{
		UpdateInterval: time.Second,
		MaxPriceAge:    time.Minute,
		Providers:      providerCfgs,
		FallbackDenoms: []string{"uusd", "ukrw"},
	}
}

func testResolverConfig(denom, routeName string, pairs ...types.Pair) resolverpkg.Config {
	return resolverpkg.Config{
		Routes: map[string][]resolverpkg.Route{
			denom: {
				{
					Name:  routeName,
					Pairs: pairs,
				},
			},
		},
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

func requireOracleStarted(t *testing.T, oracle *Runtime) {
	t.Helper()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatal("oracle did not start")
		case <-ticker.C:
			if oracle.IsRunning() {
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
