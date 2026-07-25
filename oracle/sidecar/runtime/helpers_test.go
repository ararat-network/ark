package runtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"ark/oracle/sidecar/chainstate"
	"ark/oracle/sidecar/providers"
	binanceapi "ark/oracle/sidecar/providers/api/binance"
	"ark/oracle/sidecar/providers/base"
	"ark/oracle/sidecar/providers/base/api"
	basetestutil "ark/oracle/sidecar/providers/base/testutil"
	"ark/oracle/sidecar/providers/base/websocket"
	providertypes "ark/oracle/sidecar/providers/types"
	resolverpkg "ark/oracle/sidecar/resolver"
	. "ark/oracle/sidecar/runtime"
	oracletestutil "ark/oracle/sidecar/runtime/testutil"
	"ark/oracle/sidecar/types"
)

type mockProvider struct {
	provider *base.Provider
	fetcher  *basetestutil.MockFetcher
}

func withInitialProviders(initial ...*base.Provider) Option {
	remaining := make(map[string]*base.Provider, len(initial))
	for _, provider := range initial {
		remaining[provider.Name()] = provider
	}

	return WithProviderFactory(func(
		cfg providers.Config,
		markets providertypes.Markets,
	) (*base.Provider, error) {
		if provider, ok := remaining[cfg.Name]; ok {
			delete(remaining, cfg.Name)
			provider.UpdateMarkets(markets)
			return provider, nil
		}

		return providers.NewProvider(cfg, markets, log.NewNopLogger())
	})
}

type recordingChainStateClient struct {
	mut           sync.Mutex
	startOnce     sync.Once
	started       chan struct{}
	updates       []chainstate.Config
	stoppedCalled bool
}

func newRecordingChainStateClient(
	t *testing.T,
	ctrl *gomock.Controller,
) (*oracletestutil.MockChainStateClient, *recordingChainStateClient) {
	t.Helper()

	recorder := &recordingChainStateClient{started: make(chan struct{})}
	client := oracletestutil.NewMockChainStateClient(ctrl)
	client.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			recorder.recordStart()
			<-ctx.Done()
			recorder.recordStop()
			return ctx.Err()
		}).
		AnyTimes()
	client.EXPECT().
		Update(gomock.Any()).
		Do(recorder.recordUpdate).
		AnyTimes()
	client.EXPECT().
		VoteTargets().
		Return([]string{"ausd"}, nil).
		AnyTimes()

	return client, recorder
}

func newPassthroughChainStateClient(
	t *testing.T,
	ctrl *gomock.Controller,
) *oracletestutil.MockChainStateClient {
	t.Helper()

	client, _ := newRecordingChainStateClient(t, ctrl)
	return client
}

func expectVoteTargetsLifecycle(client *oracletestutil.MockChainStateClient) {
	client.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}).
		AnyTimes()
}

func (c *recordingChainStateClient) recordStart() {
	c.startOnce.Do(func() {
		close(c.started)
	})
}

func (c *recordingChainStateClient) recordStop() {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.stoppedCalled = true
}

func (c *recordingChainStateClient) recordUpdate(cfg chainstate.Config) {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.updates = append(c.updates, cfg)
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
		t.Fatal("chain state client did not start")
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
		MaxPriceAge:   time.Minute,
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
		{Pair: "NOAH/USD", Symbol: "NOAHUSD"},
		{Pair: "NOAH/KRW", Symbol: "NOAHKRW"},
	}
}

func testRouteMarkets() providertypes.Markets {
	return providertypes.Markets{
		{Pair: "NOAH/USD", Symbol: "NOAHUSD"},
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
		MaxPriceAge:   time.Minute,
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
		MaxTickersPerConnection:  1,
		MaxSubscriptionsPerBatch: 1,
	}
}

func testOracleConfig(providerCfgs map[string]providers.Config) Config {
	return Config{
		UpdateInterval: time.Second,
		Providers:      providerCfgs,
		Client: chainstate.Config{
			Address:  "passthrough:///vote-targets",
			Timeout:  time.Second,
			Interval: time.Second,
		},
		FallbackDenoms: []string{"ausd", "akrw"},
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

func startOracle(t *testing.T, oracle *Runtime) (<-chan error, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Run(ctx)
	}()
	requireOracleStarted(t, oracle)

	return errCh, cancel
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
