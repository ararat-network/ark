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

	"github.com/ararat-network/ark/pkg/tlsconfig"
	"github.com/ararat-network/ark/pricefeed/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/chainstate"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	frankfurterapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	baseapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	runtimepkg "github.com/ararat-network/ark/pricefeed/sidecar/runtime"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

type serverTestFetcher struct {
	pricesByTicker map[string]*big.Float
}

func newServerTestFetcher(prices sidecartypes.Prices) *serverTestFetcher {
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

func (*staticChainStateClient) Update(chainstate.Config) error { return nil }

func (c *staticChainStateClient) Feeds() ([]string, error) {
	return append([]string(nil), c.feeds...), nil
}

type blockingFeedsClient struct {
	*staticChainStateClient

	armed       atomic.Bool
	blocked     chan struct{}
	releaseCh   chan struct{}
	releaseOnce sync.Once
}

func newBlockingFeedsClient(feeds []string) *blockingFeedsClient {
	return &blockingFeedsClient{
		staticChainStateClient: newStaticChainStateClient(feeds),
		blocked:                make(chan struct{}),
		releaseCh:              make(chan struct{}),
	}
}

// Feeds blocks the first aggregation tick after arm until release.
func (c *blockingFeedsClient) Feeds() ([]string, error) {
	if c.armed.CompareAndSwap(true, false) {
		close(c.blocked)
		<-c.releaseCh
	}
	return c.staticChainStateClient.Feeds()
}

func (c *blockingFeedsClient) arm() {
	c.armed.Store(true)
}

func (c *blockingFeedsClient) release() {
	c.releaseOnce.Do(func() {
		close(c.releaseCh)
	})
}

func newTestOracle(t *testing.T, prices sidecartypes.Prices) *Service {
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
) *Service {
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
	oracle := &Service{
		runtime:           runtime,
		runtimeConfigPath: process.RuntimeConfigPath,
		logger:            logger.With("component", "oracle"),
	}
	oracle.server, err = newServer(oracle, logger, process.ServerAddress, process.TLS)
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
			TLS:       tlsconfig.Client{Mode: tlsconfig.Plaintext},
			Addresses: []string{"passthrough:///feeds"},
			Timeout:   time.Second,
			Interval:  time.Hour,
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
			TLS:       tlsconfig.Client{Mode: tlsconfig.Plaintext},
			Addresses: []string{"passthrough:///oracle"},
			Timeout:   time.Second,
			Interval:  time.Second,
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

func startTestRuntime(t *testing.T, oracle *Service) {
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

// requireOracleTick waits for a committed snapshot holding every listed feed: the
// first tick can commit before the provider's first response is ingested.
func requireOracleTick(t *testing.T, oracle *Service, feeds ...string) *api.PricesResponse {
	t.Helper()

	var response *api.PricesResponse
	require.Eventually(t, func() bool {
		resp, err := oracle.Prices(context.Background(), &api.PricesRequest{})
		if err != nil || resp.Timestamp.IsZero() {
			return false
		}
		for _, feed := range feeds {
			if _, ok := resp.Prices[feed]; !ok {
				return false
			}
		}
		response = resp
		return true
	}, time.Second, time.Millisecond)
	return response
}
