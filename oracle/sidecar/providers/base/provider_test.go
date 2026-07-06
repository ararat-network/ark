package base_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log/v2"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"noah/oracle/sidecar/providers/base"
	basetestutil "noah/oracle/sidecar/providers/base/testutil"
	"noah/oracle/sidecar/providers/types"
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

func TestStartReturnsAfterStartingFetcherUntilStopped(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	expectFetcher(fetcher, "test", base.API)

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

	provider, err := newTestProviderWithMarkets(markets, fetcher)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.Start(context.Background())
	}()
	require.NoError(t, requireProviderStartReturned(t, errCh))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetcher did not start")
	}

	require.True(t, provider.IsRunning())

	provider.Stop()

	require.False(t, provider.IsRunning())
}

func TestStartIsIdempotentWhileProviderIsRunning(t *testing.T) {
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

	require.NoError(t, provider.Start(context.Background()))
	require.NoError(t, provider.Start(context.Background()))
	requireSignal(t, started, "provider did not start")

	provider.Stop()
	require.False(t, provider.IsRunning())
}

func TestStartUsesFetcherResponseBufferSize(t *testing.T) {
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

	provider, err := newTestProviderWithMarkets(markets, fetcher)
	require.NoError(t, err)

	require.NoError(t, provider.Start(context.Background()))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fetcher did not start")
	}

	provider.Stop()
}

func TestStartStopsAfterNonContextFetcherError(t *testing.T) {
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)

	tickers := []types.Ticker{"ATOMUSD"}
	markets := testMarkets()
	fetchErr := errors.New("fetch failed")
	started := make(chan struct{})

	expectFetcher(fetcher, "test", base.API)
	fetcher.EXPECT().
		Run(gomock.Any(), tickers, gomock.Any()).
		DoAndReturn(func(context.Context, []types.Ticker, chan<- types.Response) error {
			close(started)
			return fetchErr
		})

	provider, err := newTestProviderWithMarkets(markets, fetcher)
	require.NoError(t, err)

	require.NoError(t, provider.Start(context.Background()))
	requireSignal(t, started, "provider did not start")
	require.Eventually(t, func() bool {
		return !provider.IsRunning()
	}, time.Second, time.Millisecond)
}

func TestStartLogsUnexpectedProviderExitWithoutError(t *testing.T) {
	logs := &lockedBuffer{}
	logger := log.NewLogger(logs)
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)

	expectFetcher(fetcher, "test", base.API)
	started := make(chan struct{})
	fetcher.EXPECT().
		Run(gomock.Any(), []types.Ticker{"ATOMUSD"}, gomock.Any()).
		DoAndReturn(func(context.Context, []types.Ticker, chan<- types.Response) error {
			close(started)
			return nil
		})

	provider, err := newTestProvider(fetcher, base.WithLogger(logger))
	require.NoError(t, err)

	require.NoError(t, provider.Start(context.Background()))
	requireSignal(t, started, "provider did not start")
	require.Eventually(t, func() bool {
		return !provider.IsRunning()
	}, time.Second, time.Millisecond)

	require.Contains(t, logs.String(), "provider exited")
	require.Contains(t, logs.String(), "without error")
}

func TestStopDoesNotLogIntentionalStopAsProviderExited(t *testing.T) {
	logs := &lockedBuffer{}
	logger := log.NewLogger(logs)
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

	provider, err := newTestProvider(fetcher, base.WithLogger(logger))
	require.NoError(t, err)

	require.NoError(t, provider.Start(context.Background()))
	requireSignal(t, started, "provider did not start")

	provider.Stop()

	require.NotContains(t, logs.String(), "provider exited")
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
	transportType base.TransportType,
	fetcher base.Fetcher,
	opts ...base.Option,
) (*base.Provider, error) {
	return base.NewProvider("test", transportType, testMarkets(), fetcher, opts...)
}

func testMarkets() types.Markets {
	return types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}}
}

func requireProviderStartReturned(t *testing.T, errCh <-chan error) error {
	t.Helper()

	select {
	case err := <-errCh:
		return err
	case <-time.After(time.Second):
		t.Fatal("provider start did not return")
		return nil
	}
}

type lockedBuffer struct {
	mut sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mut.Lock()
	defer b.mut.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mut.Lock()
	defer b.mut.Unlock()

	return b.buf.String()
}
