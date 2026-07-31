package sidecar

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"ark/oracle/sidecar/chainstate"
	"ark/oracle/sidecar/providers"
	frankfurterapi "ark/oracle/sidecar/providers/api/frankfurter"
	"ark/oracle/sidecar/providers/base"
	baseapi "ark/oracle/sidecar/providers/base/api"
	providertypes "ark/oracle/sidecar/providers/types"
	runtimepkg "ark/oracle/sidecar/runtime"
	oracletypes "ark/oracle/sidecar/types"
	transporttypes "ark/oracle/types"
)

type serverTestFetcher struct {
	pricesByTicker map[string]*big.Float
}

func newServerTestFetcher(prices oracletypes.Prices) *serverTestFetcher {
	pricesByTicker := make(map[string]*big.Float, len(prices))
	markets := newTestMarkets()
	for pair, price := range prices {
		ticker, ok := markets.PairToTicker(pair)
		if !ok || price == nil {
			continue
		}
		pricesByTicker[ticker.Key()] = new(big.Float).Copy(price)
	}

	return &serverTestFetcher{pricesByTicker: pricesByTicker}
}

func (f *serverTestFetcher) Run(
	ctx context.Context,
	tickers []providertypes.Ticker,
	responseCh chan<- providertypes.Response,
) error {
	resolved := make(map[providertypes.Ticker]providertypes.Result)
	now := time.Now().UTC()
	for _, ticker := range tickers {
		price, ok := f.pricesByTicker[ticker.Key()]
		if !ok {
			continue
		}
		resolved[ticker] = providertypes.NewResult(new(big.Float).Copy(price), now)
	}
	if len(resolved) > 0 {
		select {
		case responseCh <- providertypes.NewResponse(resolved, nil):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	<-ctx.Done()
	return ctx.Err()
}

func (*serverTestFetcher) Name() string {
	return "test"
}

func (*serverTestFetcher) ResponseBufferSize([]providertypes.Ticker) int {
	return 1
}

func (*serverTestFetcher) Type() base.TransportType {
	return base.API
}

type staticChainStateClient struct {
	feeds []string
}

func newStaticChainStateClient(feeds []string) *staticChainStateClient {
	return &staticChainStateClient{feeds: append([]string(nil), feeds...)}
}

func (*staticChainStateClient) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (*staticChainStateClient) Update(chainstate.Config) {}

func (c *staticChainStateClient) Feeds() ([]string, error) {
	return append([]string(nil), c.feeds...), nil
}

type blockingFeedsClient struct {
	*staticChainStateClient

	calls       atomic.Int64
	blocked     chan struct{}
	releaseCh   chan struct{}
	blockedOnce sync.Once
	releaseOnce sync.Once
}

func newBlockingFeedsClient(feeds []string) *blockingFeedsClient {
	return &blockingFeedsClient{
		staticChainStateClient: newStaticChainStateClient(feeds),
		blocked:                make(chan struct{}),
		releaseCh:              make(chan struct{}),
	}
}

func (c *blockingFeedsClient) Feeds() ([]string, error) {
	if c.calls.Add(1) == 2 {
		c.blockedOnce.Do(func() {
			close(c.blocked)
		})
		<-c.releaseCh
	}
	return c.staticChainStateClient.Feeds()
}

func (c *blockingFeedsClient) release() {
	c.releaseOnce.Do(func() {
		close(c.releaseCh)
	})
}

func newTestOracle(t *testing.T, prices oracletypes.Prices) *Oracle {
	t.Helper()

	cfg := newTestRuntimeConfig()
	return newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(prices),
		newStaticChainStateClient(cfg.FallbackFeeds),
		ProcessConfig{ServerAddress: "127.0.0.1:0"},
	)
}

func newTestOracleFromRuntime(
	t *testing.T,
	cfg runtimepkg.Config,
	fetcher base.Fetcher,
	client runtimepkg.ChainStateClient,
	process ProcessConfig,
) *Oracle {
	t.Helper()

	providerFactory := func(
		providerCfg providers.Config,
		markets providertypes.Markets,
	) (*base.Provider, error) {
		return base.NewProvider(
			providerCfg.Name,
			providerCfg.TransportType,
			markets,
			fetcher,
		)
	}
	runtime, err := runtimepkg.NewRuntime(
		cfg,
		runtimepkg.WithProviderFactory(providerFactory),
		runtimepkg.WithChainStateClient(client),
	)
	require.NoError(t, err)

	logger := log.NewNopLogger()
	if process.ServerAddress == "" {
		process.ServerAddress = defaultServerAddress
	}
	oracle := &Oracle{
		runtime:           runtime,
		runtimeConfigPath: process.RuntimeConfigPath,
		logger:            logger.With("component", "oracle"),
	}
	oracle.server, err = newServer(oracle, logger, process.ServerAddress)
	require.NoError(t, err)
	return oracle
}

func newTestRuntimeConfig() runtimepkg.Config {
	markets := newTestMarkets()
	return runtimepkg.Config{
		UpdateInterval: 10 * time.Millisecond,
		Providers: map[string]providers.Config{
			"test": {
				Name:          "test",
				TransportType: base.API,
				Markets:       markets,
				MaxPriceAge:   time.Minute,
				API: baseapi.Config{
					Name:      "test",
					Timeout:   time.Second,
					Interval:  time.Hour,
					Endpoints: []providertypes.Endpoint{{URL: "https://example.invalid/prices"}},
				},
			},
		},
		Client: chainstate.Config{
			Address:  "passthrough:///feeds",
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		FallbackFeeds: []string{"ausd", "akrw"},
	}
}

func testInternalRuntimeConfig() runtimepkg.Config {
	providerCfg := providers.Config{
		Name:          frankfurterapi.Name,
		TransportType: base.API,
		Markets: providertypes.Markets{
			{Pair: "NOAH/USD", Symbol: "NOAHUSD"},
		},
		MaxPriceAge: time.Minute,
		API:         frankfurterapi.DefaultAPIConfig,
	}

	return runtimepkg.Config{
		UpdateInterval: time.Second,
		Providers: map[string]providers.Config{
			providerCfg.Name: providerCfg,
		},
		Client: chainstate.Config{
			Address:  "passthrough:///oracle",
			Timeout:  time.Second,
			Interval: time.Second,
		},
		FallbackFeeds: []string{"ausd"},
	}
}

func newTestMarkets() providertypes.Markets {
	return providertypes.Markets{
		{Pair: "NOAH/USD", Symbol: "NOAHUSD"},
		{Pair: "NOAH/KRW", Symbol: "NOAHKRW"},
	}
}

func startTestRuntime(t *testing.T, oracle *Oracle) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.runtime.Run(ctx)
	}()
	require.Eventually(t, oracle.runtime.IsRunning, time.Second, time.Millisecond)
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("runtime did not stop")
		}
	})
}

func requireOracleTick(t *testing.T, oracle *Oracle) *transporttypes.OraclePricesResponse {
	t.Helper()

	var response *transporttypes.OraclePricesResponse
	require.Eventually(t, func() bool {
		resp, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})
		if err != nil || resp.Timestamp.IsZero() {
			return false
		}
		response = resp
		return true
	}, time.Second, time.Millisecond)
	return response
}
