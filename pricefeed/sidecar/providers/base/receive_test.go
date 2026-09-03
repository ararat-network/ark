package base_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	basetestutil "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/testutil"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	oracletypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func TestProviderStoresResultAndGetPricesReturnsDeepCopy(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, testMarkets(), fetcher)

	seedProviderPrices(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
		},
		nil,
	), 1)

	prices := provider.GetPrices()
	require.Equal(t, 0, prices[oracletypes.Pair("ATOM/USD")].Price.Cmp(big.NewFloat(12.34)))

	prices[oracletypes.Pair("ATOM/USD")].Price.SetFloat64(99)

	prices = provider.GetPrices()
	require.Equal(t, 0, prices[oracletypes.Pair("ATOM/USD")].Price.Cmp(big.NewFloat(12.34)))
}

func TestProviderIgnoresOlderResults(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, testMarkets(), fetcher)
	currentTime := time.Unix(20, 0).UTC()

	seedProviderResponses(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, []types.Response{
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(2), currentTime),
		}, nil),
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		}, nil),
	}, func() bool {
		prices := provider.GetPrices()
		price, ok := prices[oracletypes.Pair("ATOM/USD")]
		return ok && price.Timestamp.Equal(currentTime) && price.Price.Cmp(big.NewFloat(2)) == 0
	})
}

func TestProviderIgnoresUnchangedResultWithoutExistingPrice(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, testMarkets(), fetcher)

	seedProviderPrices(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"ATOMUSD": types.NewUnchangedResult(time.Unix(10, 0).UTC()),
		},
		nil,
	), 0)
}

func TestProviderIgnoresResolvedResultWithoutPrice(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, testMarkets(), fetcher)

	seedProviderPrices(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(nil, time.Unix(10, 0).UTC()),
		},
		nil,
	), 0)
}

func TestProviderIgnoresResultsWithoutMarketMapping(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, testMarkets(), fetcher)

	seedProviderPrices(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, types.NewResponse(
		map[types.Ticker]types.Result{
			"BTCUSD": types.NewResult(big.NewFloat(2), time.Unix(10, 0).UTC()),
		},
		nil,
	), 0)
}

func TestProviderRefreshesTimestampForUnchangedResult(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, testMarkets(), fetcher)
	currentTime := time.Unix(10, 0).UTC()
	updatedTime := time.Unix(20, 0).UTC()

	seedProviderResponses(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, []types.Response{
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(2), currentTime),
		}, nil),
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewUnchangedResult(updatedTime),
		}, nil),
	}, func() bool {
		prices := provider.GetPrices()
		price, ok := prices[oracletypes.Pair("ATOM/USD")]
		return ok &&
			price.Timestamp.Equal(updatedTime) &&
			price.LastObserved.Equal(currentTime) &&
			!price.Unchanged &&
			price.Price.Cmp(big.NewFloat(2)) == 0
	})
}

// TestProviderResetsLastObservedOnRealResult pins the other half of the
// bookkeeping: a real price after an unchanged refresh moves both timestamps,
// so the bound measures from the newest observation rather than the first.
func TestProviderResetsLastObservedOnRealResult(t *testing.T) {
	fetcher := newMockFetcher(t)
	provider := newProvider(t, testMarkets(), fetcher)
	firstTime := time.Unix(10, 0).UTC()
	unchangedTime := time.Unix(20, 0).UTC()
	secondTime := time.Unix(30, 0).UTC()

	seedProviderResponses(t, provider, fetcher, []types.Ticker{"ATOMUSD"}, []types.Response{
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(2), firstTime),
		}, nil),
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewUnchangedResult(unchangedTime),
		}, nil),
		types.NewResponse(map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(3), secondTime),
		}, nil),
	}, func() bool {
		prices := provider.GetPrices()
		price, ok := prices[oracletypes.Pair("ATOM/USD")]
		return ok &&
			price.Timestamp.Equal(secondTime) &&
			price.LastObserved.Equal(secondTime) &&
			price.Price.Cmp(big.NewFloat(3)) == 0
	})
}

func seedProviderResponses(
	t *testing.T,
	provider *base.Provider,
	fetcher *basetestutil.MockFetcher,
	tickers []types.Ticker,
	responses []types.Response,
	waitFor func() bool,
) {
	t.Helper()

	started := make(chan struct{})
	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, responseCh chan<- types.Response) error {
			close(started)
			for _, response := range responses {
				select {
				case responseCh <- response:
				case <-ctx.Done():
					return ctx.Err()
				}
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
	require.Eventually(t, waitFor, time.Second, time.Millisecond)

	cancel()
	require.ErrorIs(t, requireProviderRunReturned(t, errCh), context.Canceled)
}
