package base_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	sidecarinternal "github.com/ararat-network/ark/pricefeed/sidecar/internal"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	basetestutil "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/testutil"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestRunRejectsNilContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()

	provider, err := newTestProvider(fetcher)
	require.NoError(t, err)

	var ctx context.Context
	err = provider.Run(ctx)
	require.ErrorContains(t, err, "context cannot be nil")
}

func TestRunBlocksUntilContextCancellation(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.API)

	started := make(chan struct{})
	fetcher.EXPECT().
		Run(gomock.Any(), []types.Ticker{"ATOMUSD"}, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, _ chan<- types.Response) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})

	provider, err := newTestProvider(fetcher)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.Run(ctx)
	}()
	requireSignal(t, started, "provider did not start")

	select {
	case err := <-errCh:
		t.Fatalf("provider Run returned before cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	require.ErrorIs(t, requireProviderRunReturned(t, errCh), context.Canceled)
}

func TestRunUsesFetcherResponseBufferSize(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().Name().Return("test").AnyTimes()
	fetcher.EXPECT().Type().Return(base.API).AnyTimes()

	tickers := []types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}
	markets := types.Markets{
		{Pair: "ATOM/USD", Symbol: "ATOMUSD"},
		{Pair: "BTC/USD", Symbol: "BTCUSD"},
		{Pair: "ETH/USD", Symbol: "ETHUSD"},
	}
	fetcher.EXPECT().ResponseBufferSize(tickers).Return(2)
	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []types.Ticker, responseCh chan<- types.Response) error {
			require.Equal(t, 2, cap(responseCh))
			return nil
		})

	provider, err := newTestProviderWithMarkets(markets, fetcher)
	require.NoError(t, err)
	require.NoError(t, provider.Run(context.Background()))
}

func TestRunReturnsFetcherError(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.API)

	fetchErr := errors.New("fetch failed")
	fetcher.EXPECT().
		Run(gomock.Any(), []types.Ticker{"ATOMUSD"}, gomock.Any()).
		Return(fetchErr)

	provider, err := newTestProvider(fetcher)
	require.NoError(t, err)
	require.ErrorIs(t, provider.Run(context.Background()), fetchErr)
}

func TestRunReturnsRecoveredFetcherPanic(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.API)
	fetcher.EXPECT().
		Run(gomock.Any(), []types.Ticker{"ATOMUSD"}, gomock.Any()).
		DoAndReturn(func(context.Context, []types.Ticker, chan<- types.Response) error {
			panic("fetch exploded")
		})

	provider, err := newTestProvider(fetcher)
	require.NoError(t, err)

	err = provider.Run(context.Background())
	require.True(t, sidecarinternal.IsPanic(err))
	require.ErrorContains(t, err, "provider fetcher panicked: fetch exploded")
}

func TestRunWaitsForCancellationWhenMarketsAreEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.API)

	provider, err := newTestProviderWithMarkets(nil, fetcher)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.Run(ctx)
	}()

	select {
	case err := <-errCh:
		t.Fatalf("provider Run returned before cancellation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	require.ErrorIs(t, requireProviderRunReturned(t, errCh), context.Canceled)
}

func TestTypeReturnsConfiguredType(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.WebSocket)

	provider, err := newTestProviderWithType(base.WebSocket, fetcher)
	require.NoError(t, err)
	require.Equal(t, base.WebSocket, provider.Type())
}

func TestNewProviderAllowsEmptyMarkets(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.API)

	provider, err := base.NewProvider("test", base.API, nil, fetcher)
	require.NoError(t, err)
	require.Empty(t, provider.GetTickers())
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
	fetcher.EXPECT().Name().Return(name).AnyTimes()
	fetcher.EXPECT().Type().Return(providerType).AnyTimes()
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
	transportType base.TransportType,
	fetcher base.Fetcher,
	opts ...base.Option,
) (*base.Provider, error) {
	return base.NewProvider("test", transportType, testMarkets(), fetcher, opts...)
}

func testMarkets() types.Markets {
	return types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}}
}

func requireProviderRunReturned(t *testing.T, errCh <-chan error) error {
	t.Helper()

	select {
	case err := <-errCh:
		return err
	case <-time.After(time.Second):
		t.Fatal("provider Run did not return")
		return nil
	}
}
