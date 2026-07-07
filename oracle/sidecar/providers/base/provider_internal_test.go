package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers/types"
)

func TestRunCancelsBeforeLifecycleCleanupLock(t *testing.T) {
	cancelCalled := make(chan struct{})
	started := make(chan struct{})
	allowReturn := make(chan struct{})
	runErr := errors.New("fetch stopped")

	mainCtx, realCancel := context.WithCancel(context.Background())
	defer realCancel()
	mainCancel := func() {
		close(cancelCalled)
		realCancel()
	}
	doneCh := make(chan struct{})
	provider := &Provider{
		logger:     log.NewNopLogger(),
		fetcher:    lifecycleFetcher{started: started, allowReturn: allowReturn, err: runErr},
		markets:    types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		mainCtx:    mainCtx,
		cancelMain: mainCancel,
		doneCh:     doneCh,
	}

	finished := make(chan struct{})
	go func() {
		provider.run(mainCtx, mainCancel, doneCh)
		close(finished)
	}()
	requireSignal(t, started, "fetcher did not start")

	provider.lifecycleMu.Lock()
	close(allowReturn)
	select {
	case <-cancelCalled:
	case <-time.After(time.Second):
		t.Fatal("main context was not canceled while lifecycle lock was held")
	}
	select {
	case <-doneCh:
		t.Fatal("done channel closed before lifecycle state cleanup acquired lock")
	case <-time.After(20 * time.Millisecond):
	}

	provider.lifecycleMu.Unlock()
	requireSignal(t, finished, "provider run did not finish")
}

func TestRunClearsLifecycleState(t *testing.T) {
	mainCtx, mainCancel := context.WithCancel(context.Background())
	defer mainCancel()
	cycleCtx, cycleCancel := context.WithCancel(mainCtx)
	defer cycleCancel()
	doneCh := make(chan struct{})
	started := make(chan struct{})
	allowReturn := make(chan struct{})
	provider := &Provider{
		logger:      log.NewNopLogger(),
		fetcher:     lifecycleFetcher{started: started, allowReturn: allowReturn, err: errors.New("fetch stopped")},
		markets:     types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		mainCtx:     mainCtx,
		cancelMain:  mainCancel,
		cycleCtx:    cycleCtx,
		cancelCycle: cycleCancel,
		doneCh:      doneCh,
	}

	finished := make(chan struct{})
	go func() {
		provider.run(mainCtx, mainCancel, doneCh)
		close(finished)
	}()
	requireSignal(t, started, "fetcher did not start")
	close(allowReturn)
	requireSignal(t, finished, "provider run did not finish")

	if provider.mainCtx != nil {
		t.Fatal("main context was not cleared")
	}
	if provider.cancelMain != nil {
		t.Fatal("main cancel function was not cleared")
	}
	if provider.cycleCtx != nil {
		t.Fatal("cycle context was not cleared")
	}
	if provider.cancelCycle != nil {
		t.Fatal("cycle cancel function was not cleared")
	}
	if provider.doneCh != nil {
		t.Fatal("done channel was not cleared")
	}
	select {
	case <-doneCh:
	default:
		t.Fatal("done channel was not closed")
	}
}

func TestRunRepanicsFetcherPanicAfterLifecycleCleanup(t *testing.T) {
	mainCtx, mainCancel := context.WithCancel(context.Background())
	defer mainCancel()
	doneCh := make(chan struct{})
	provider := &Provider{
		logger:     log.NewNopLogger(),
		fetcher:    panicLifecycleFetcher{},
		markets:    types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		mainCtx:    mainCtx,
		cancelMain: mainCancel,
		doneCh:     doneCh,
	}

	require.PanicsWithError(t, "provider fetcher panicked: fetch exploded", func() {
		provider.run(mainCtx, mainCancel, doneCh)
	})
	if provider.mainCtx != nil {
		t.Fatal("main context was not cleared")
	}
	if provider.doneCh != nil {
		t.Fatal("done channel was not cleared")
	}
	select {
	case <-doneCh:
	default:
		t.Fatal("done channel was not closed")
	}
}

type lifecycleFetcher struct {
	started     chan<- struct{}
	allowReturn <-chan struct{}
	err         error
}

func (f lifecycleFetcher) Run(
	_ context.Context,
	_ []types.Ticker,
	_ chan<- types.Response,
) error {
	close(f.started)
	<-f.allowReturn
	return f.err
}

func (f lifecycleFetcher) Name() string { return "test" }

func (f lifecycleFetcher) ResponseBufferSize([]types.Ticker) int { return 0 }

func (f lifecycleFetcher) Type() TransportType { return API }

type panicLifecycleFetcher struct{}

func (panicLifecycleFetcher) Run(
	context.Context,
	[]types.Ticker,
	chan<- types.Response,
) error {
	panic("fetch exploded")
}

func (panicLifecycleFetcher) Name() string { return "test" }

func (panicLifecycleFetcher) ResponseBufferSize([]types.Ticker) int { return 1 }

func (panicLifecycleFetcher) Type() TransportType { return API }

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}
