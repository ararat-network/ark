package providertest

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"noah/oracle/providers/base"
	"noah/oracle/providers/types"
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
		require.Contains(t, result.Prices, "ubtc")
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

	markets := make(types.Markets, 0, len(tickers))
	denoms := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		denom := "u" + string(ticker)
		if ticker == "BTCUSDT" {
			denom = "ubtc"
		}
		if ticker == "ETHUSDT" {
			denom = "ueth"
		}

		denoms = append(denoms, denom)
		markets = append(markets, types.Market{
			Denom:  denom,
			Symbol: ticker,
		})
	}

	return base.NewProvider(base.Config{
		Name:    "test",
		Type:    base.API,
		Markets: markets,
	}, stubFetcher{
		name:    "test",
		tickers: tickers,
		results: results,
	}, base.WithDenoms(denoms))
}

type stubFetcher struct {
	name    string
	tickers []types.Ticker
	results map[types.Ticker]types.Result
}

func (f stubFetcher) Run(ctx context.Context, tickers []types.Ticker, responseCh chan<- types.Response) error {
	resolved := make(map[types.Ticker]types.Result, len(f.results))
	for ticker, result := range f.results {
		resolved[ticker] = result
	}

	select {
	case responseCh <- types.NewResponse(resolved, nil):
	case <-ctx.Done():
		return ctx.Err()
	}

	<-ctx.Done()
	return ctx.Err()
}

func (f stubFetcher) Name() string {
	return f.name
}

func (f stubFetcher) ResponseBufferSize([]types.Ticker) int {
	return len(f.tickers)
}

func (f stubFetcher) Type() base.TransportType {
	return base.API
}
