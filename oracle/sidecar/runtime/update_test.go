package runtime_test

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"noah/oracle/sidecar/chainstate"
	"noah/oracle/sidecar/providers"
	binanceapi "noah/oracle/sidecar/providers/api/binance"
	"noah/oracle/sidecar/providers/base"
	"noah/oracle/sidecar/providers/base/api"
	basetestutil "noah/oracle/sidecar/providers/base/testutil"
	"noah/oracle/sidecar/providers/base/websocket"
	providertypes "noah/oracle/sidecar/providers/types"
	resolverpkg "noah/oracle/sidecar/resolver"
	. "noah/oracle/sidecar/runtime"
	oracletestutil "noah/oracle/sidecar/runtime/testutil"
	"noah/oracle/sidecar/types"
)

func TestUpdateConfigDoesNotStopExistingProviderWhenReplacementBuildFails(t *testing.T) {
	markets := providertypes.Markets{{Pair: "ARK/USD", Symbol: "ARKUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", markets)
	expectFetcherRun(provider.fetcher, started)

	require.NoError(t, provider.provider.Start(context.Background()))
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

	err = oracle.Update(newCfg)
	require.ErrorContains(t, err, "unrecognised provider name")
	require.True(t, provider.provider.IsRunning())
	require.Same(t, provider.provider, oracle.GetProviders()["unknown"])

	provider.provider.Stop()
}

func TestUpdateConfigDoesNotMutateRuntimeStateWhenProviderPlanFails(t *testing.T) {
	markets := testRouteMarkets()
	providerCfg := testUnknownAPIProviderConfig("unknown", markets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", markets)
	resolver := newRecordingResolver(types.Prices{
		"ARK/USD": big.NewFloat(1.25),
		"ARK/KRW": big.NewFloat(1300),
	})
	client := newRecordingChainStateClient()
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(resolver),
		WithChainStateClient(client),
	)
	require.NoError(t, err)

	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := cfg
	newCfg.Providers = map[string]providers.Config{
		"unknown": newProviderCfg,
	}
	newCfg.Resolver = testResolverConfig("ukrw", "ark-krw", "ARK/USD", "USD/KRW")
	newCfg.Client.Interval = 10 * time.Millisecond
	newCfg.FallbackDenoms = []string{"uusd"}

	err = oracle.Update(newCfg)

	require.ErrorContains(t, err, "unrecognised provider name")
	require.Empty(t, resolver.updateConfigs())
	require.Empty(t, client.updateConfigs())
	require.Same(t, provider.provider, oracle.GetProviders()["unknown"])
	require.Contains(t, oracle.GetPrices(), "ukrw")
}

func TestUpdateConfigInjectsMarketOnlyChangeWithoutRebuildingProvider(t *testing.T) {
	oldMarkets := providertypes.Markets{{Pair: "ARK/USD", Symbol: "ARKUSD"}}
	newMarkets := providertypes.Markets{{Pair: "ARK/USD", Symbol: "BUSDUSD"}}
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

	require.NoError(t, oracle.Update(newCfg))
	require.Same(t, provider.provider, oracle.GetProviders()["unknown"])
	require.Equal(t, newMarkets.Tickers(), provider.provider.GetTickers())
}

func TestUpdateConfigRestartsStoppedProviderOnMarketOnlyChange(t *testing.T) {
	oldMarkets := providertypes.Markets{{Pair: "ARK/USD", Symbol: "ARKUSD"}}
	newMarkets := providertypes.Markets{{Pair: "ARK/USD", Symbol: "BUSDUSD"}}
	providerCfg := testUnknownAPIProviderConfig("unknown", oldMarkets)
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", oldMarkets)
	firstStarted := make(chan struct{})
	restarted := make(chan struct{})
	expectFetcherRunErrorThenBlock(
		t,
		provider.fetcher,
		firstStarted,
		restarted,
		oldMarkets.Tickers(),
		newMarkets.Tickers(),
	)
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(newRecordingResolver(nil)),
		WithChainStateClient(newRecordingChainStateClient()),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()
	requireProviderStarted(t, firstStarted)
	require.Eventually(t, func() bool {
		return !provider.provider.IsRunning()
	}, time.Second, time.Millisecond)

	newProviderCfg := providerCfg
	newProviderCfg.Markets = newMarkets
	newCfg := testOracleConfig(map[string]providers.Config{
		"unknown": newProviderCfg,
	})
	require.NoError(t, oracle.Update(newCfg))

	requireProviderStarted(t, restarted)
	require.Same(t, provider.provider, oracle.GetProviders()["unknown"])
	require.Equal(t, newMarkets.Tickers(), provider.provider.GetTickers())

	cancel()
	requireOracleStopped(t, errCh)
}

func TestUpdateConfigRebuildsTargetProviderMissingFromRuntimeState(t *testing.T) {
	markets := testMarkets()
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	ctrl := gomock.NewController(t)
	staleProvider := newMockProvider(t, ctrl, "stale", markets)
	oracle, err := NewRuntime(
		cfg,
		WithProviders(staleProvider.provider),
		WithResolver(oracletestutil.NewMockPriceResolver(ctrl)),
	)
	require.NoError(t, err)
	require.NotContains(t, oracle.GetProviders(), providerCfg.Name)

	require.NoError(t, oracle.Update(cfg))

	providers := oracle.GetProviders()
	require.Contains(t, providers, providerCfg.Name)
	require.NotContains(t, providers, "stale")
	require.Equal(t, markets.Tickers(), providers[providerCfg.Name].GetTickers())
}

func TestUpdateConfigAppliesUpdateIntervalWithoutRestart(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})
	cfg.UpdateInterval = time.Hour
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	started := make(chan struct{})
	expectFetcherRun(provider.fetcher, started)
	resolver := oracletestutil.NewMockPriceResolver(ctrl)
	aggregateCh := make(chan struct{}, 1)
	resolver.EXPECT().Reset().AnyTimes()
	resolver.EXPECT().SetProviderPrices(gomock.Any(), gomock.Any()).AnyTimes()
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

	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(resolver),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Start(ctx)
	}()
	requireOracleStarted(t, oracle)
	requireProviderStarted(t, started)

	newCfg := cfg
	newCfg.UpdateInterval = 10 * time.Millisecond
	require.NoError(t, oracle.Update(newCfg))

	select {
	case <-aggregateCh:
	case <-time.After(time.Second):
		t.Fatal("oracle did not apply updated interval")
	}

	cancel()
	requireOracleStopped(t, errCh)
}

func TestUpdateConfigUpdatesResolverConfig(t *testing.T) {
	markets := testRouteMarkets()
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", markets),
	})
	newCfg := cfg
	newCfg.Resolver = testResolverConfig("ukrw", "ark-krw", "ARK/USD", "USD/KRW")

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", markets)
	resolver := oracletestutil.NewMockPriceResolver(ctrl)
	resolver.EXPECT().Update(newCfg.Resolver)
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(resolver),
	)
	require.NoError(t, err)

	require.NoError(t, oracle.Update(newCfg))
}

func TestUpdateConfigReturnsInvalidResolverErrorWithoutChangingConfig(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})
	newCfg := cfg
	newCfg.Resolver = testResolverConfig("ukrw", "bad-route", "USDT/USD")

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	resolver := newRecordingResolver(types.Prices{
		"ARK/USD": big.NewFloat(1.25),
		"ARK/KRW": big.NewFloat(1300),
	})
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(resolver),
	)
	require.NoError(t, err)

	err = oracle.Update(newCfg)

	require.ErrorContains(t, err, "resolver denom \"ukrw\" route \"bad-route\" resolves to \"USDT/USD\", want \"ARK/KRW\"")
	require.Contains(t, oracle.GetPrices(), "ukrw")
}

func TestUpdateConfigUpdatesVoteTargetsClientConfig(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})
	newCfg := cfg
	newCfg.Client.Interval = 10 * time.Millisecond

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	voteTargetsClient := newRecordingChainStateClient()
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	require.NoError(t, oracle.Update(newCfg))
	require.Equal(t, []chainstate.Config{newCfg.Client}, voteTargetsClient.updateConfigs())
}

func TestUpdateConfigRefreshesFallbackDenomsWhenNoVoteTargetsHaveLoaded(t *testing.T) {
	providerCfg := testUnknownAPIProviderConfig("unknown", testMarkets())
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	resolver := newRecordingResolver(types.Prices{
		"ARK/USD": big.NewFloat(1.25),
		"ARK/KRW": big.NewFloat(1300),
	})
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(resolver),
	)
	require.NoError(t, err)

	newCfg := cfg
	newCfg.FallbackDenoms = []string{"uusd"}

	require.NoError(t, oracle.Update(newCfg))
	require.Equal(t, types.DenomPrices{"uusd": big.NewFloat(1.25)}, oracle.GetPrices())
	require.Equal(t, []providertypes.Ticker{"ARKUSD"}, provider.provider.GetTickers())
}

func TestUpdateConfigKeepsProviderWhenFallbackDenomsDeactivateMarkets(t *testing.T) {
	providerCfg := testUnknownAPIProviderConfig("unknown", testMarkets())
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": providerCfg,
	})

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(oracletestutil.NewMockPriceResolver(ctrl)),
	)
	require.NoError(t, err)

	newCfg := cfg
	newCfg.FallbackDenoms = []string{"ueur"}

	require.NoError(t, oracle.Update(newCfg))
	require.Contains(t, oracle.GetProviders(), "unknown")
	require.Empty(t, provider.provider.GetTickers())
}

func TestStartStartsAndStopsVoteTargetsClient(t *testing.T) {
	cfg := testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})

	ctrl := gomock.NewController(t)
	started := make(chan struct{})
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	expectFetcherRun(provider.fetcher, started)

	voteTargetsClient := newRecordingChainStateClient()
	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithChainStateClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		errCh <- oracle.Start(ctx)
	}()
	requireOracleStarted(t, oracle)
	requireProviderStarted(t, started)
	voteTargetsClient.requireStarted(t)

	cancel()
	requireOracleStopped(t, errCh)
	require.True(t, voteTargetsClient.stopped())
}

func TestStopWaitsForConcurrentConfigUpdateProviderStops(t *testing.T) {
	markets := testMarkets()
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, providerCfg.Name, markets)
	started := make(chan struct{})
	stopStarted := make(chan struct{})
	allowStop := make(chan struct{})
	defer func() {
		select {
		case <-allowStop:
		default:
			close(allowStop)
		}
	}()
	provider.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, _ chan<- providertypes.Response) error {
			close(started)
			<-ctx.Done()
			close(stopStarted)
			<-allowStop
			return ctx.Err()
		})

	oracle, err := NewRuntime(
		cfg,
		WithProviders(provider.provider),
		WithResolver(newRecordingResolver(nil)),
		WithChainStateClient(newRecordingChainStateClient()),
	)
	require.NoError(t, err)
	require.NoError(t, provider.provider.Start(context.Background()))
	requireSignal(t, started, "provider did not start")

	newProviderCfg := providerCfg
	newProviderCfg.API.Interval += time.Second
	newCfg := testOracleConfig(map[string]providers.Config{
		providerCfg.Name: newProviderCfg,
	})

	updateErrCh := make(chan error, 1)
	go func() {
		updateErrCh <- oracle.Update(newCfg)
	}()
	requireSignal(t, stopStarted, "provider stop did not begin")

	stopDone := make(chan struct{})
	go func() {
		oracle.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		t.Fatal("runtime stop returned before config update provider stop finished")
	case <-time.After(20 * time.Millisecond):
	}

	close(allowStop)
	requireSignal(t, stopDone, "runtime stop did not return")
	select {
	case err := <-updateErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("runtime update did not return")
	}
}

type mockProvider struct {
	provider *base.Provider
	fetcher  *basetestutil.MockFetcher
}

type recordingChainStateClient struct {
	mut           sync.Mutex
	started       chan struct{}
	updates       []chainstate.Config
	stoppedCalled bool
}

func newRecordingChainStateClient() *recordingChainStateClient {
	return &recordingChainStateClient{started: make(chan struct{})}
}

func (c *recordingChainStateClient) Start(context.Context) error {
	close(c.started)
	return nil
}

func (c *recordingChainStateClient) Stop() {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.stoppedCalled = true
}

func (c *recordingChainStateClient) Update(cfg chainstate.Config) {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.updates = append(c.updates, cfg)
}

func (c *recordingChainStateClient) VoteTargets() ([]string, error) {
	return []string{"uusd"}, nil
}

func (c *recordingChainStateClient) updateConfigs() []chainstate.Config {
	c.mut.Lock()
	defer c.mut.Unlock()

	return append([]chainstate.Config(nil), c.updates...)
}

func (c *recordingChainStateClient) stopped() bool {
	c.mut.Lock()
	defer c.mut.Unlock()

	return c.stoppedCalled
}

func (c *recordingChainStateClient) requireStarted(t *testing.T) {
	t.Helper()

	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("vote-target client did not start")
	}
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

func expectFetcherRunAnyTimes(fetcher *basetestutil.MockFetcher, started chan<- struct{}) {
	var once sync.Once
	fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, _ chan<- providertypes.Response) error {
			once.Do(func() {
				close(started)
			})
			<-ctx.Done()
			return ctx.Err()
		}).
		AnyTimes()
}

func expectFetcherRunErrorThenBlock(
	t *testing.T,
	fetcher *basetestutil.MockFetcher,
	firstStarted chan<- struct{},
	restarted chan<- struct{},
	firstTickers []providertypes.Ticker,
	restartedTickers []providertypes.Ticker,
) {
	t.Helper()

	runErr := errors.New("fetch failed")
	var mu sync.Mutex
	calls := 0
	fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, tickers []providertypes.Ticker, _ chan<- providertypes.Response) error {
			mu.Lock()
			calls++
			call := calls
			mu.Unlock()

			switch call {
			case 1:
				require.Equal(t, firstTickers, tickers)
				close(firstStarted)
				return runErr
			case 2:
				require.Equal(t, restartedTickers, tickers)
				close(restarted)
				<-ctx.Done()
				return ctx.Err()
			default:
				require.Failf(t, "unexpected fetcher run", "call %d", call)
				return nil
			}
		}).
		Times(2)
}

func testUnknownAPIProviderConfig(name string, markets providertypes.Markets) providers.Config {
	return providers.Config{
		Name:          name,
		TransportType: base.API,
		Markets:       markets,
		API: api.Config{
			Name:      name,
			Timeout:   time.Second,
			Interval:  time.Second,
			Endpoints: []providertypes.Endpoint{{URL: "https://example.invalid/prices"}},
		},
	}
}

func testMarkets() providertypes.Markets {
	return providertypes.Markets{
		{Pair: "ARK/USD", Symbol: "ARKUSD"},
		{Pair: "ARK/KRW", Symbol: "ARKKRW"},
	}
}

func testRouteMarkets() providertypes.Markets {
	return providertypes.Markets{
		{Pair: "ARK/USD", Symbol: "ARKUSD"},
		{Pair: "USD/KRW", Symbol: "USDKRW"},
	}
}

func testRuntimeConfigWithUnknownProvider() Config {
	return testOracleConfig(map[string]providers.Config{
		"unknown": testUnknownAPIProviderConfig("unknown", testMarkets()),
	})
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
		Client: chainstate.Config{
			Address:  "passthrough:///vote-targets",
			Timeout:  time.Second,
			Interval: time.Second,
		},
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

	requireSignal(t, started, "provider did not start")
}

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
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
