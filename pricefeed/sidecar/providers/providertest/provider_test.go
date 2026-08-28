package providertest_test

import (
	. "ark/pricefeed/sidecar/providers/providertest"
	"context"
	"maps"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"ark/pricefeed/sidecar/providers/base"
	basetestutil "ark/pricefeed/sidecar/providers/base/testutil"
	"ark/pricefeed/sidecar/providers/types"
	oracletypes "ark/pricefeed/sidecar/types"
)

func TestRunBuildsProviderAndCollectsPriceSnapshots(t *testing.T) {
	results, err := Run(context.Background(), func(context.Context) (*base.Provider, error) {
		return newTestProvider(t, []types.Ticker{"BTCUSDT"}, map[types.Ticker]types.Result{
			"BTCUSDT": types.NewResult(big.NewFloat(100), time.Now().UTC()),
		})
	}, Config{
		TestDuration:   3 * time.Millisecond,
		PollInterval:   time.Millisecond,
		BurnInInterval: 0,
	})

	require.NoError(t, err)
	require.NotEmpty(t, results)

	for _, result := range results {
		require.Len(t, result.Prices, 1)
		require.Contains(t, result.Prices, oracletypes.Pair("BTC/USD"))
	}
}

func TestRunProviderReturnsErrorWhenExpectedPricesAreMissing(t *testing.T) {
	provider, err := newTestProvider(t, []types.Ticker{"BTCUSDT", "ETHUSDT"}, map[types.Ticker]types.Result{
		"BTCUSDT": types.NewResult(big.NewFloat(100), time.Now().UTC()),
	})
	require.NoError(t, err)

	results, err := RunProvider(context.Background(), provider, Config{
		TestDuration:   3 * time.Millisecond,
		PollInterval:   time.Millisecond,
		BurnInInterval: 0,
	})

	require.ErrorContains(t, err, "expected 2 prices, got 1")
	require.Nil(t, results)
}

func newTestProvider(
	t *testing.T,
	tickers []types.Ticker,
	results map[types.Ticker]types.Result,
) (*base.Provider, error) {
	t.Helper()

	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()
	fetcher.EXPECT().ResponseBufferSize(gomock.Any()).Return(len(tickers)).AnyTimes()
	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, responseCh chan<- types.Response) error {
			resolved := make(map[types.Ticker]types.Result, len(results))
			maps.Copy(resolved, results)

			select {
			case responseCh <- types.NewResponse(resolved, nil):
			case <-ctx.Done():
				return ctx.Err()
			}

			<-ctx.Done()
			return ctx.Err()
		})

	markets := make(types.Markets, 0, len(tickers))
	for _, ticker := range tickers {
		pair := oracletypes.Pair(string(ticker) + "/USD")
		if ticker == "BTCUSDT" {
			pair = "BTC/USD"
		}
		if ticker == "ETHUSDT" {
			pair = "ETH/USD"
		}

		markets = append(markets, types.Market{
			Pair:   pair,
			Symbol: ticker,
		})
	}

	return base.NewProvider("test", base.API, markets, fetcher)
}
