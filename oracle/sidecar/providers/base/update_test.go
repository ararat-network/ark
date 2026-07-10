package base_test

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	base "ark/oracle/sidecar/providers/base"
	basetestutil "ark/oracle/sidecar/providers/base/testutil"
	"ark/oracle/sidecar/providers/types"
	oracletypes "ark/oracle/sidecar/types"
)

func TestGetTickersReturnsCopy(t *testing.T) {
	provider := newProvider(t, types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}}, newMockFetcher(t))

	tickers := provider.GetTickers()
	tickers[0] = "BTCUSD"

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestUpdateMarketsRetainsPricesForUnchangedMarketsAndPrunesRemovedMarkets(t *testing.T) {
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

	provider.UpdateMarkets(types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}})

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
	prices := provider.GetPrices()
	require.Len(t, prices, 1)
	require.Contains(t, prices, oracletypes.Pair("ATOM/USD"))
	require.NotContains(t, prices, oracletypes.Pair("USDT/USD"))
	require.Zero(t, prices[oracletypes.Pair("ATOM/USD")].Price.Cmp(big.NewFloat(12.34)))
}

func TestUpdateMarketsRetainsCaseOnlySymbolChangeAndAppliesNewerObservation(t *testing.T) {
	fetcher := newMockFetcher(t)
	pair := oracletypes.Pair("ATOM/USD")
	provider := newProvider(t, types.Markets{{Pair: pair, Symbol: "ATOMUSD"}}, fetcher)
	oldTimestamp := time.Unix(10, 0).UTC()
	newTimestamp := time.Unix(20, 0).UTC()

	seedProviderResponses(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, []types.Response{
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(1), oldTimestamp),
		}, nil),
	}, func() bool {
		result, ok := provider.GetPrices()[pair]
		return ok && result.Timestamp.Equal(oldTimestamp)
	})

	provider.UpdateMarkets(types.Markets{{Pair: pair, Symbol: "atomusd"}})

	require.Equal(t, []types.Ticker{"atomusd"}, provider.GetTickers())
	retained := provider.GetPrices()[pair]
	require.Equal(t, oldTimestamp, retained.Timestamp)
	require.Zero(t, retained.Price.Cmp(big.NewFloat(1)))

	seedProviderResponses(t, provider, fetcher, []types.Ticker{"atomusd"}, []types.Response{
		types.NewResponse(map[types.Ticker]types.Result{
			"atomusd": types.NewResult(big.NewFloat(2), newTimestamp),
		}, nil),
	}, func() bool {
		result, ok := provider.GetPrices()[pair]
		return ok &&
			result.Price != nil &&
			result.Timestamp.Equal(newTimestamp) &&
			result.Price.Cmp(big.NewFloat(2)) == 0
	})

	// A case-only symbol change must not leave two raw ticker entries competing
	// to project onto the same pair.
	for range 1000 {
		result := provider.GetPrices()[pair]
		require.Equal(t, newTimestamp, result.Timestamp)
		require.Zero(t, result.Price.Cmp(big.NewFloat(2)))
	}
}

func TestUpdateMarketsReplacesMarketsAndTickers(t *testing.T) {
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "OLDUSD"}}, newMockFetcher(t))
	markets := types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}

	provider.UpdateMarkets(markets)

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
}

func TestUpdateMarketsCopiesInput(t *testing.T) {
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "OLDUSD"}}, newMockFetcher(t))
	markets := types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}

	provider.UpdateMarkets(markets)
	markets[0].Symbol = "MUTATED"

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
}

func TestMarketsValidateRejectsDuplicateSymbolsBeforeUpdate(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}, fetcher)
	seedProviderPrices(t, provider, fetcher, []types.Ticker{"USDTUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
		nil,
	), 1)

	markets := types.Markets{
		{Pair: "USDT/USD", Symbol: "USDTUSD"},
		{Pair: "USDC/USD", Symbol: "usdtusd"},
	}
	err := markets.Validate()

	require.ErrorContains(t, err, `duplicate symbol "USDTUSD"`)
	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.Contains(t, provider.GetPrices(), oracletypes.Pair("USDT/USD"))
}

func TestUpdateMarketsClearsCachedPricesForPairRemap(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}, fetcher)
	seedProviderPrices(t, provider, fetcher, []types.Ticker{"USDTUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
		nil,
	), 1)

	provider.UpdateMarkets(types.Markets{{Pair: "USDC/USD", Symbol: "USDTUSD"}})

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.Empty(t, provider.GetPrices())
}

func TestUpdateMarketsClearsCachedPricesForSymbolRemap(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, types.Markets{{Pair: "USDT/USD", Symbol: "USDTUSD"}}, fetcher)
	seedProviderPrices(t, provider, fetcher, []types.Ticker{"USDTUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
		nil,
	), 1)

	provider.UpdateMarkets(types.Markets{{Pair: "USDT/USD", Symbol: "USDCUSD"}})

	require.Equal(t, []types.Ticker{"USDCUSD"}, provider.GetTickers())
	require.Empty(t, provider.GetPrices())
}

func TestUpdateMarketsCanRunWhileProviderReadsState(t *testing.T) {
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
		for i := range iterations {
			markets := types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}}
			if i%2 == 0 {
				markets = types.Markets{
					{Pair: "ATOM/USD", Symbol: "ATOMUSD"},
					{Pair: "BTC/USD", Symbol: "BTCUSD"},
				}
			}
			provider.UpdateMarkets(markets)
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for range iterations {
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
		errCh <- provider.Run(ctx)
	}()

	requireSignal(t, started, "provider did not start")
	require.Eventually(t, func() bool {
		return len(provider.GetPrices()) == wantPrices
	}, time.Second, time.Millisecond)

	cancel()
	require.ErrorIs(t, requireProviderRunReturned(t, errCh), context.Canceled)
}

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.Fail(t, message)
	}
}
