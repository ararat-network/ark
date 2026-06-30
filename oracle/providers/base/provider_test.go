package base_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"noah/oracle/providers/base"
	basetestutil "noah/oracle/providers/base/testutil"
	"noah/oracle/providers/types"
)

func TestStartRejectsNilContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()

	provider, err := newTestProvider(fetcher)
	require.NoError(t, err)

	var ctx context.Context
	err = provider.Start(ctx)
	require.ErrorContains(t, err, "context cannot be nil")
}

func TestStartExitsWhenNoTickersAreConfigured(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()

	provider, err := newTestProvider(fetcher, base.WithDenoms([]string{"umissing"}))
	require.NoError(t, err)

	require.NoError(t, provider.Start(context.Background()))
	require.False(t, provider.IsRunning())
}

func TestStartRunsFetcherUntilStopped(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.API)

	denoms := []string{"uatom"}
	tickers := []types.Ticker{"ATOMUSD"}
	markets := testMarkets()
	started := make(chan struct{})

	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, _ chan<- types.Response) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})

	provider, err := newTestProviderWithMarkets(markets, fetcher, base.WithDenoms(denoms))
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.Start(context.Background())
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetcher did not start")
	}

	require.True(t, provider.IsRunning())

	provider.Stop()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("provider did not stop")
	}
	require.False(t, provider.IsRunning())
}

func TestStartUsesFetcherResponseBufferSize(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()

	denoms := []string{"uatom", "ubtc", "ueth"}
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}
	markets := types.Markets{
		{Denom: "uatom", Symbol: "ATOMUSD"},
		{Denom: "ubtc", Symbol: "BTCUSD"},
		{Denom: "ueth", Symbol: "ETHUSD"},
	}
	started := make(chan struct{})

	fetcher.EXPECT().
		ResponseBufferSize(tickers).
		Return(2)
	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, responseCh chan<- types.Response) error {
			require.Equal(t, 2, cap(responseCh))
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})

	provider, err := newTestProviderWithMarkets(markets, fetcher, base.WithDenoms(denoms))
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.Start(context.Background())
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetcher did not start")
	}

	provider.Stop()
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("provider did not stop")
	}
}

func TestStartReturnsNonContextFetcherError(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)

	denoms := []string{"uatom"}
	tickers := []types.Ticker{"ATOMUSD"}
	markets := testMarkets()
	fetchErr := errors.New("fetch failed")

	expectFetcher(fetcher, "test", base.API)
	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		Return(fetchErr)

	provider, err := newTestProviderWithMarkets(markets, fetcher, base.WithDenoms(denoms))
	require.NoError(t, err)

	require.ErrorIs(t, provider.Start(context.Background()), fetchErr)
	require.False(t, provider.IsRunning())
}

func TestTypeReturnsConfiguredType(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.WebSocket)

	provider, err := newTestProviderWithType(base.WebSocket, fetcher)
	require.NoError(t, err)

	require.Equal(t, base.WebSocket, provider.Type())
}

func TestNewProviderRejectsMismatchedFetcherName(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "other", base.API)

	provider, err := newTestProvider(fetcher)

	require.Nil(t, provider)
	require.ErrorContains(t, err, "mismatched provider and fetcher name")
}

func newTestProvider(fetcher base.Fetcher, opts ...base.Option) (*base.Provider, error) {
	return newTestProviderWithMarkets(testMarkets(), fetcher, opts...)
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
		DoAndReturn(func(tickers []types.Ticker) int {
			return len(tickers)
		}).
		AnyTimes()
}

func newTestProviderWithMarkets(
	markets types.Markets,
	fetcher base.Fetcher,
	opts ...base.Option,
) (*base.Provider, error) {
	return base.NewProvider("test", base.API, markets, fetcher, opts...)
}

func newTestProviderWithType(
	providerType base.TransportType,
	fetcher base.Fetcher,
	opts ...base.Option,
) (*base.Provider, error) {
	return base.NewProvider("test", providerType, testMarkets(), fetcher, opts...)
}

func testMarkets() types.Markets {
	return types.Markets{{Denom: "uatom", Symbol: "ATOMUSD"}}
}
