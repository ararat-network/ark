package base_test

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	base "noah/oracle/sidecar/providers/base"
	basetestutil "noah/oracle/sidecar/providers/base/testutil"
	"noah/oracle/sidecar/providers/types"
	oracletypes "noah/oracle/sidecar/types"
)

func TestUpdateCancelsFetchContextAndReplacesTickersFromMarkets(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{
		{Pair: "ATOM/USD", Symbol: "ATOMUSD"},
	}, fetcher)

	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})

	fetcher.EXPECT().
		Run(gomock.Any(), []types.Ticker{"ATOMUSD"}, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, _ chan<- types.Response) error {
			close(firstStarted)
			<-ctx.Done()
			close(firstCanceled)
			return ctx.Err()
		})
	fetcher.EXPECT().
		Run(gomock.Any(), []types.Ticker{"ATOMUSD", "BTCUSD"}, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, _ chan<- types.Response) error {
			close(secondStarted)
			select {
			case <-releaseSecond:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})

	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.Start(context.Background())
	}()

	requireSignal(t, firstStarted, "provider did not start")

	markets := types.Markets{
		{Pair: "ATOM/USD", Symbol: "ATOMUSD"},
		{Pair: "BTC/USD", Symbol: "BTCUSD"},
	}
	require.NoError(t, provider.Update(base.WithNewMarkets(markets)))

	requireSignal(t, firstCanceled, "fetch cycle was not canceled")
	requireSignal(t, secondStarted, "provider did not restart")
	require.Equal(t, []types.Ticker{"ATOMUSD", "BTCUSD"}, provider.GetTickers())

	close(releaseSecond)
	require.NoError(t, <-errCh)
}

func TestGetTickersReturnsCopy(t *testing.T) {
	provider := newProvider(t, types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}}, newMockFetcher(t))

	tickers := provider.GetTickers()
	tickers[0] = "BTCUSD"

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestWithNewMarketsRetainsPricesForUnchangedMarketsAndPrunesRemovedMarkets(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{
		{Pair: "ATOM/USD", Symbol: "ATOMUSD"},
		{Pair: "USDT/USD", Symbol: "USDTUSD"},
	}, fetcher)
	seedProviderPrices(t, provider, fetcher, []types.Ticker{"ATOMUSD", "USDTUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
		nil,
	), 2)

	require.NoError(t, provider.Update(base.WithNewMarkets(types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}})))

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
	prices := provider.GetPrices()
	require.Len(t, prices, 1)
	require.Contains(t, prices, oracletypes.Pair("ATOM/USD"))
	require.NotContains(t, prices, oracletypes.Pair("USDT/USD"))
	require.Zero(t, prices[oracletypes.Pair("ATOM/USD")].Price.Cmp(big.NewFloat(12.34)))
}

func TestWithNewMarketsReplacesMarketsAndTickers(t *testing.T) {
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "OLDUSD"}}, newMockFetcher(t))
	markets := types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}

	require.NoError(t, provider.Update(base.WithNewMarkets(markets)))

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
}

func TestWithNewMarketsCopiesInput(t *testing.T) {
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "OLDUSD"}}, newMockFetcher(t))
	markets := types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}

	require.NoError(t, provider.Update(base.WithNewMarkets(markets)))
	markets[0].Symbol = "MUTATED"

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
}

func TestWithNewMarketsRejectsInvalidAndLeavesStateUnchanged(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}, fetcher)
	seedProviderPrices(t, provider, fetcher, []types.Ticker{"USDTUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
		nil,
	), 1)

	err := provider.Update(base.WithNewMarkets(types.Markets{
		{Pair: "USDT/USD", Symbol: "USDTUSD"},
		{Pair: "USDC/USD", Symbol: "usdtusd"},
	}))

	require.ErrorContains(t, err, `duplicate symbol "USDTUSD"`)
	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.Contains(t, provider.GetPrices(), oracletypes.Pair("USDT/USD"))
}

func TestWithNewMarketsClearsCachedPricesForPairRemap(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}, fetcher)
	seedProviderPrices(t, provider, fetcher, []types.Ticker{"USDTUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
		nil,
	), 1)

	require.NoError(t, provider.Update(base.WithNewMarkets(types.Markets{{Pair: "USDC/USD", Symbol: "USDTUSD"}})))

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.Empty(t, provider.GetPrices())
}

func TestWithNewMarketsClearsCachedPricesForSymbolRemap(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}, fetcher)
	seedProviderPrices(t, provider, fetcher, []types.Ticker{"USDTUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
		nil,
	), 1)

	require.NoError(t, provider.Update(base.WithNewMarkets(types.Markets{{Pair: "USDT/USD", Symbol: "USDCUSD"}})))

	require.Equal(t, []types.Ticker{"USDCUSD"}, provider.GetTickers())
	require.Empty(t, provider.GetPrices())
}

func TestWithNewMarketsCanRunWhileProviderReadsState(t *testing.T) {
	provider := newProvider(t, types.Markets{
		{Pair: "ATOM/USD", Symbol: "ATOMUSD"},
		{Pair: "BTC/USD", Symbol: "BTCUSD"},
	}, newMockFetcher(t))

	const iterations = 1000
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			markets := types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}}
			if i%2 == 0 {
				markets = types.Markets{
					{Pair: "ATOM/USD", Symbol: "ATOMUSD"},
					{Pair: "BTC/USD", Symbol: "BTCUSD"},
				}
			}
			require.NoError(t, provider.Update(base.WithNewMarkets(markets)))
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < iterations; i++ {
			_ = provider.Name()
			_ = provider.Type()
			_ = provider.GetPrices()
			_ = provider.GetTickers()
		}
	}()

	close(start)
	wg.Wait()
}

func newMockFetcher(t *testing.T) *basetestutil.MockFetcher {
	t.Helper()

	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()
	fetcher.EXPECT().
		ResponseBufferSize(gomock.Any()).
		DoAndReturn(func(tickers []types.Ticker) int {
			return len(tickers)
		}).
		AnyTimes()

	return fetcher
}

func newProvider(t *testing.T, markets types.Markets, fetcher *basetestutil.MockFetcher) *base.Provider {
	t.Helper()

	provider, err := base.NewProvider("test", base.API, markets, fetcher)
	require.NoError(t, err)

	return provider
}

func seedProviderPrices(
	t *testing.T,
	provider *base.Provider,
	fetcher *basetestutil.MockFetcher,
	tickers []types.Ticker,
	response types.Response,
	wantPrices int,
) {
	t.Helper()

	started := make(chan struct{})
	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, responseCh chan<- types.Response) error {
			close(started)
			select {
			case responseCh <- response:
			case <-ctx.Done():
				return ctx.Err()
			}

			<-ctx.Done()
			return ctx.Err()
		})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.Start(ctx)
	}()

	requireSignal(t, started, "provider did not start")
	require.Eventually(t, func() bool {
		return len(provider.GetPrices()) == wantPrices
	}, time.Second, time.Millisecond)

	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.Fail(t, message)
	}
}
