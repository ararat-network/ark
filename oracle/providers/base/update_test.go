package base

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers/types"
)

func TestUpdateCancelsFetchContextAndReplacesTickersFromDenoms(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets: types.Markets{
			{Denom: "uatom", Symbol: "ATOMUSD"},
			{Denom: "ubtc", Symbol: "BTCUSD"},
		},
	}
	fetchCtx, fetchCancel := context.WithCancel(context.Background())
	provider.setCycleCtx(fetchCtx, fetchCancel)
	defer fetchCancel()

	denoms := []string{"uatom", "ubtc"}
	require.NoError(t, provider.Update(WithNewDenoms(denoms)))

	require.ErrorIs(t, fetchCtx.Err(), context.Canceled)
	require.Equal(t, []types.Ticker{"ATOMUSD", "BTCUSD"}, provider.GetTickers())
	require.Equal(t, denoms, provider.denoms)
}

func TestGetTickersReturnsCopy(t *testing.T) {
	provider := &Provider{
		tickers: []types.Ticker{"ATOMUSD"},
	}

	tickers := provider.GetTickers()
	tickers[0] = "BTCUSD"

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestWithNewDenomsCopiesInput(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets:      types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
	}
	denoms := []string{"uatom"}

	require.NoError(t, provider.Update(WithNewDenoms(denoms)))
	denoms[0] = "ubtc"

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestWithNewDenomsIgnoresEmptyAndClearsPrices(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets: types.Markets{
			{Denom: "uatom", Symbol: "ATOMUSD"},
		},
		denoms:  []string{"uatom"},
		tickers: []types.Ticker{"ATOMUSD"},
		prices: map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
		},
	}
	fetchCtx, fetchCancel := context.WithCancel(context.Background())
	provider.setCycleCtx(fetchCtx, fetchCancel)
	defer fetchCancel()

	require.NoError(t, provider.Update(WithNewDenoms(nil)))

	require.ErrorIs(t, fetchCtx.Err(), context.Canceled)
	require.Equal(t, []string{"uatom"}, provider.denoms)
	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
	require.Empty(t, provider.GetPrices())
}

func TestWithNewDenomsClearsPricesForRemovedTickers(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets: types.Markets{
			{Denom: "uatom", Symbol: "ATOMUSD"},
			{Denom: "uusd", Symbol: "USDTUSD"},
		},
		denoms:  []string{"uatom", "uusd"},
		tickers: []types.Ticker{"ATOMUSD", "USDTUSD"},
		prices: map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
	}

	require.NoError(t, provider.Update(WithNewDenoms([]string{"uatom"})))

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
	require.Empty(t, provider.GetPrices())
}

func TestWithNewDenomsClearsCachedPrices(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets: types.Markets{
			{Denom: "uatom", Symbol: "ATOMUSD"},
			{Denom: "uusd", Symbol: "USDTUSD"},
		},
		denoms:  []string{"uatom"},
		tickers: []types.Ticker{"ATOMUSD"},
		prices: map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
		},
	}

	require.NoError(t, provider.Update(WithNewDenoms([]string{"uatom", "uusd"})))

	require.Empty(t, provider.GetPrices())
}

func TestWithNewMarketsReplacesMarketsAndTickers(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets:      types.Markets{{Denom: "uusd", Symbol: "OLDUSD"}},
		denoms:       []string{"uusd"},
		tickers:      []types.Ticker{"OLDUSD"},
	}
	markets := types.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}

	require.NoError(t, provider.Update(WithNewMarkets(markets)))

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.Equal(t, markets, provider.markets)
}

func TestWithNewMarketsCopiesInput(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets:      types.Markets{{Denom: "uusd", Symbol: "OLDUSD"}},
		denoms:       []string{"uusd"},
		tickers:      []types.Ticker{"OLDUSD"},
	}
	markets := types.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}

	require.NoError(t, provider.Update(WithNewMarkets(markets)))
	markets[0].Symbol = "MUTATED"

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
}

func TestWithNewMarketsRejectsInvalidAndLeavesStateUnchanged(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets:      types.Markets{{Denom: "uusd", Symbol: "USDTUSD"}},
		denoms:       []string{"uusd"},
		tickers:      []types.Ticker{"USDTUSD"},
		prices: map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
	}
	fetchCtx, fetchCancel := context.WithCancel(context.Background())
	provider.setCycleCtx(fetchCtx, fetchCancel)
	defer fetchCancel()

	err := provider.Update(WithNewMarkets(types.Markets{
		{Denom: "uusd", Symbol: "USDTUSD"},
		{Denom: "uusdc", Symbol: "usdtusd"},
	}))

	require.ErrorContains(t, err, `duplicate symbol "USDTUSD"`)
	require.NoError(t, fetchCtx.Err())
	require.Equal(t, types.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}, provider.markets)
	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.NotEmpty(t, provider.GetPrices())
}

func TestWithNewMarketsClearsCachedPricesForTickerRemap(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets:      types.Markets{{Denom: "uusd", Symbol: "USDTUSD"}},
		denoms:       []string{"uusd"},
		tickers:      []types.Ticker{"USDTUSD"},
		prices: map[types.Ticker]types.Result{
			"USDTUSD": types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()),
		},
	}

	require.NoError(t, provider.Update(
		WithNewDenoms([]string{"uusdc"}),
		WithNewMarkets(types.Markets{{Denom: "uusdc", Symbol: "USDTUSD"}}),
	))

	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.Empty(t, provider.GetPrices())
}

func TestWithNewDenomsCanRunWhileProviderReadsState(t *testing.T) {
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      newStubFetcher(API),
		name:         "test",
		providerType: API,
		markets: types.Markets{
			{Denom: "uatom", Symbol: "ATOMUSD"},
			{Denom: "ubtc", Symbol: "BTCUSD"},
		},
		denoms:  []string{"uatom", "ubtc"},
		tickers: []types.Ticker{"ATOMUSD", "BTCUSD"},
		prices: map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
			"BTCUSD":  types.NewResult(big.NewFloat(56.78), time.Unix(10, 0).UTC()),
		},
	}

	const iterations = 1000
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for i := range iterations {
			denoms := []string{"uatom"}
			if i%2 == 0 {
				denoms = []string{"uatom", "ubtc"}
			}
			require.NoError(t, provider.Update(WithNewDenoms(denoms)))
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

type stubFetcher struct {
	name        string
	fetcherType TransportType
}

func newStubFetcher(fetcherType TransportType) stubFetcher {
	return stubFetcher{
		name:        "test",
		fetcherType: fetcherType,
	}
}

func (f stubFetcher) Run(context.Context, []types.Ticker, chan<- types.Response) error {
	return nil
}

func (f stubFetcher) Name() string {
	return f.name
}

func (f stubFetcher) ResponseBufferSize(tickers []types.Ticker) int {
	return len(tickers)
}

func (f stubFetcher) Type() TransportType {
	return f.fetcherType
}
