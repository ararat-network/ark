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
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name: "test",
			Type: API,
			Markets: types.Markets{
				{Denom: "uatom", Symbol: "ATOMUSD"},
				{Denom: "ubtc", Symbol: "BTCUSD"},
			},
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
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name:    "test",
			Type:    API,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
		},
	}
	denoms := []string{"uatom"}

	require.NoError(t, provider.Update(WithNewDenoms(denoms)))
	denoms[0] = "ubtc"

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestUpdateResolvesTickersAfterAllOptionsRun(t *testing.T) {
	newFetcher := newStubFetcher(WebSocket)
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name: "test",
			Type: API,
			Markets: types.Markets{
				{Denom: "uusd", Symbol: "OLDUSD"},
			},
		},
		denoms:  []string{"uusd"},
		tickers: []types.Ticker{"OLDUSD"},
	}
	newMarkets := types.Markets{
		{Denom: "uusd", Symbol: "USDTUSD"},
	}

	require.NoError(t, provider.Update(
		WithNewDenoms([]string{"uusd"}),
		WithNewConfig(Config{
			Name:    "test",
			Type:    WebSocket,
			Markets: newMarkets,
		}),
		WithNewFetcher(newFetcher),
	))

	require.Equal(t, WebSocket, provider.Type())
	require.Equal(t, []string{"uusd"}, provider.denoms)
	require.Equal(t, newMarkets, provider.getConfig().Markets)
	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
}

func TestWithNewDenomsCanRunWhileProviderConfigChanges(t *testing.T) {
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name: "test",
			Type: API,
			Markets: types.Markets{
				{Denom: "uatom", Symbol: "ATOMUSD"},
				{Denom: "uusd", Symbol: "USDTUSD"},
			},
		},
		tickers: []types.Ticker{"ATOMUSD"},
	}
	apiMarkets := types.Markets{
		{Denom: "uatom", Symbol: "ATOMUSD"},
		{Denom: "uusd", Symbol: "USDTUSD"},
	}
	websocketMarkets := types.Markets{
		{Denom: "uatom", Symbol: "ATOMUSDT"},
		{Denom: "uusd", Symbol: "USDTUSDT"},
	}

	const iterations = 1000
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			require.NoError(t, provider.Update(WithNewDenoms([]string{"uatom", "uusd"})))
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for i := range iterations {
			fetcherType := API
			markets := apiMarkets
			if i%2 == 0 {
				fetcherType = WebSocket
				markets = websocketMarkets
			}
			require.NoError(t, provider.Update(
				WithNewConfig(Config{
					Name:    "test",
					Type:    fetcherType,
					Markets: markets,
				}),
				WithNewFetcher(newStubFetcher(fetcherType)),
			))
		}
	}()

	close(start)
	wg.Wait()
}

func TestWithNewFetcherAndConfigReplaceFetcherMarketsAndTickers(t *testing.T) {
	oldFetcher := newStubFetcher(API)
	newFetcher := newStubFetcher(WebSocket)
	markets := types.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: oldFetcher,
		config: Config{
			Name:    "test",
			Type:    API,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
		},
		denoms:  []string{"uusd"},
		tickers: []types.Ticker{"ATOMUSD"},
	}
	fetchCtx, fetchCancel := context.WithCancel(context.Background())
	provider.setCycleCtx(fetchCtx, fetchCancel)
	defer fetchCancel()

	require.NoError(t, provider.Update(
		WithNewConfig(Config{
			Name:    "test",
			Type:    WebSocket,
			Markets: markets,
		}),
		WithNewFetcher(newFetcher),
	))

	require.ErrorIs(t, fetchCtx.Err(), context.Canceled)
	require.Equal(t, WebSocket, provider.Type())
	require.Equal(t, []types.Ticker{"USDTUSD"}, provider.GetTickers())
	require.Equal(t, markets, provider.getConfig().Markets)
}

func TestWithNewFetcherPreservesCachedPrices(t *testing.T) {
	newFetcher := newStubFetcher(API)
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name: "test",
			Type: API,
			Markets: types.Markets{
				{Denom: "uatom", Symbol: "ATOMUSD"},
			},
		},
		denoms:  []string{"uusd"},
		tickers: []types.Ticker{"ATOMUSD"},
		prices: map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
		},
	}

	require.NoError(t, provider.Update(WithNewFetcher(newFetcher)))

	require.NotEmpty(t, provider.GetPrices())
	require.Equal(t, API, provider.Type())
}

func TestWithNewFetcherRejectsNilFetcher(t *testing.T) {
	provider := &Provider{
		logger: log.NewNopLogger(),
		config: Config{
			Name:    "test",
			Type:    API,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
		},
		fetcher: newStubFetcher(API),
	}

	err := provider.Update(WithNewFetcher(nil))

	require.ErrorContains(t, err, "fetcher is nil")
	require.Equal(t, API, provider.Type())
}

func TestUpdateRejectsMismatchedFetcherName(t *testing.T) {
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name:    "test",
			Type:    API,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
		},
		denoms:  []string{"uatom"},
		tickers: []types.Ticker{"ATOMUSD"},
	}
	fetchCtx, fetchCancel := context.WithCancel(context.Background())
	provider.setCycleCtx(fetchCtx, fetchCancel)
	defer fetchCancel()

	err := provider.Update(WithNewFetcher(stubFetcher{
		name:        "other",
		fetcherType: API,
	}))

	require.ErrorContains(t, err, "mismatched provider and fetcher name")
	require.NoError(t, fetchCtx.Err())
	require.Equal(t, "test", provider.Name())
	require.Equal(t, API, provider.Type())
	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestUpdateErrorLeavesProviderStateUnchanged(t *testing.T) {
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name:    "test",
			Type:    API,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
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

	err := provider.Update(
		WithNewConfig(Config{
			Name:    "test",
			Type:    WebSocket,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSDT"}},
		}),
		WithNewFetcher(nil),
	)

	require.ErrorContains(t, err, "fetcher is nil")
	require.NoError(t, fetchCtx.Err())
	require.Equal(t, API, provider.Type())
	require.Equal(t, types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}}, provider.getConfig().Markets)
	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
	require.NotEmpty(t, provider.GetPrices())
}

func TestConfigCloneCopiesMarkets(t *testing.T) {
	config := Config{
		Name:    "test",
		Type:    API,
		Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
	}

	clone := config.Clone()
	clone.Markets[0].Symbol = "BTCUSD"

	require.Equal(t, types.Ticker("ATOMUSD"), config.Markets[0].Symbol)
}

func TestNewProviderCopiesConfigMarkets(t *testing.T) {
	config := Config{
		Name:    "test",
		Type:    API,
		Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
	}

	provider, err := NewProvider(config, newStubFetcher(API), WithDenoms([]string{"uatom"}))
	require.NoError(t, err)

	config.Markets[0].Symbol = "BTCUSD"

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestWithNewConfigCopiesMarkets(t *testing.T) {
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name:    "test",
			Type:    API,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
		},
		denoms: []string{"uatom"},
	}
	config := Config{
		Name:    "test",
		Type:    API,
		Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
	}

	require.NoError(t, provider.Update(WithNewConfig(config)))
	config.Markets[0].Symbol = "BTCUSD"

	require.Equal(t, []types.Ticker{"ATOMUSD"}, provider.GetTickers())
}

func TestWithNewConfigPrunesPricesForRemovedMarkets(t *testing.T) {
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name: "test",
			Type: API,
			Markets: types.Markets{
				{Denom: "uatom", Symbol: "ATOMUSD"},
				{Denom: "ubtc", Symbol: "BTCUSD"},
			},
		},
		denoms:  []string{"uatom"},
		tickers: []types.Ticker{"ATOMUSD"},
		prices: map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
			"BTCUSD":  types.NewResult(big.NewFloat(56.78), time.Unix(10, 0).UTC()),
		},
	}

	require.NoError(t, provider.Update(WithNewConfig(Config{
		Name:    "test",
		Type:    API,
		Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
	})))

	prices := provider.GetPrices()
	require.Len(t, prices, 1)
	require.Contains(t, prices, "uatom")
	require.NotContains(t, prices, "ubtc")
}

func TestConfigReadsCanRunWhileConfigChanges(t *testing.T) {
	provider := &Provider{
		logger:  log.NewNopLogger(),
		fetcher: newStubFetcher(API),
		config: Config{
			Name:    "test",
			Type:    API,
			Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
		},
		prices: map[types.Ticker]types.Result{
			"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
		},
	}

	const iterations = 1000
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			require.NoError(t, provider.Update(WithNewConfig(Config{
				Name:    "test",
				Type:    API,
				Markets: types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}},
			})))
		}
	}()

	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			_ = provider.Name()
			_ = provider.Type()
			_ = provider.GetPrices()
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
