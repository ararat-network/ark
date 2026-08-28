package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	sidecarinternal "ark/pricefeed/sidecar/internal"
	"ark/pricefeed/sidecar/providers/base"
	basetestutil "ark/pricefeed/sidecar/providers/base/testutil"
	providertypes "ark/pricefeed/sidecar/providers/types"
)

func TestRunDoesNotPublishRunningBeforeInitialProvidersStart(t *testing.T) {
	started := make(chan struct{})
	ctrl := gomock.NewController(t)
	fetcher := basetestutil.NewMockFetcher(ctrl)
	fetcher.EXPECT().
		Name().
		Return("test").
		AnyTimes()
	fetcher.EXPECT().
		Type().
		Return(base.API).
		AnyTimes()
	fetcher.EXPECT().
		ResponseBufferSize(gomock.Any()).
		Return(1).
		AnyTimes()
	var startedOnce sync.Once
	fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			ctx context.Context,
			_ []providertypes.Ticker,
			_ chan<- providertypes.Response,
		) error {
			startedOnce.Do(func() {
				close(started)
			})
			<-ctx.Done()
			return ctx.Err()
		})
	provider, err := base.NewProvider(
		"test",
		base.API,
		providertypes.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		fetcher,
		base.WithLogger(log.NewNopLogger()),
	)
	require.NoError(t, err)

	oracle := &Runtime{
		logger:           log.NewNopLogger(),
		updateIntervalCh: make(chan struct{}, 1),
		cfg: Config{
			UpdateInterval: time.Hour,
		},
		providers: map[string]*managedProvider{
			provider.Name(): {provider: provider},
		},
	}

	oracle.updateMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- oracle.Run(ctx)
	}()

	require.Never(t, oracle.IsRunning, 20*time.Millisecond, time.Millisecond)
	select {
	case <-started:
		t.Fatal("provider started while runtime update lock was held")
	default:
	}

	oracle.updateMu.Unlock()

	requireSignal(t, started, "provider did not start")
	require.Eventually(t, oracle.IsRunning, time.Second, time.Millisecond)

	cancel()
	select {
	case err := <-runErrCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop")
	}
}

func TestRunReturnsProviderPanicAfterSiblingCleanup(t *testing.T) {
	siblingStarted := make(chan struct{})
	cleanupStarted := make(chan struct{})
	allowCleanup := make(chan struct{})

	ctrl := gomock.NewController(t)
	panicFetcher := basetestutil.NewMockFetcher(ctrl)
	panicFetcher.EXPECT().Name().Return("panic").AnyTimes()
	panicFetcher.EXPECT().Type().Return(base.API).AnyTimes()
	panicFetcher.EXPECT().ResponseBufferSize(gomock.Any()).Return(1)
	panicFetcher.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, []providertypes.Ticker, chan<- providertypes.Response) error {
			<-siblingStarted
			panic("fetcher exploded")
		})

	siblingFetcher := basetestutil.NewMockFetcher(ctrl)
	siblingFetcher.EXPECT().Name().Return("sibling").AnyTimes()
	siblingFetcher.EXPECT().Type().Return(base.API).AnyTimes()
	siblingFetcher.EXPECT().ResponseBufferSize(gomock.Any()).Return(1)
	siblingFetcher.EXPECT().Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, _ chan<- providertypes.Response) error {
			close(siblingStarted)
			<-ctx.Done()
			close(cleanupStarted)
			<-allowCleanup
			return ctx.Err()
		})

	panicProvider, err := base.NewProvider(
		"panic",
		base.API,
		providertypes.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		panicFetcher,
	)
	require.NoError(t, err)
	siblingProvider, err := base.NewProvider(
		"sibling",
		base.API,
		providertypes.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		siblingFetcher,
	)
	require.NoError(t, err)

	oracle := &Runtime{
		logger:           log.NewNopLogger(),
		updateIntervalCh: make(chan struct{}, 1),
		cfg:              Config{UpdateInterval: time.Hour},
		providers: map[string]*managedProvider{
			panicProvider.Name():   {provider: panicProvider},
			siblingProvider.Name(): {provider: siblingProvider},
		},
	}

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- oracle.Run(context.Background())
	}()
	requireSignal(t, cleanupStarted, "sibling cleanup did not start")

	select {
	case err := <-runErrCh:
		t.Fatalf("runtime returned before sibling cleanup completed: %v", err)
	default:
	}

	close(allowCleanup)
	select {
	case err := <-runErrCh:
		require.True(t, sidecarinternal.IsPanic(err), "expected panic-derived error, got %v", err)
		require.ErrorContains(t, err, "fetcher exploded")
	case <-time.After(time.Second):
		t.Fatal("runtime did not return after sibling cleanup")
	}
}

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.Fail(t, message)
	}
}
