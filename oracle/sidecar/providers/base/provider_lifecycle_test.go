package base

import (
	"context"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers/types"
)

func TestRunCancelsBeforeLifecycleCleanupLock(t *testing.T) {
	cancelCalled := make(chan struct{})
	started := make(chan struct{})
	allowReturn := make(chan struct{})
	runErr := errors.New("fetch stopped")

	mainCtx, realCancel := context.WithCancel(context.Background())
	mainCancel := func() {
		close(cancelCalled)
		realCancel()
	}
	doneCh := make(chan struct{})
	provider := &Provider{
		logger:       log.NewNopLogger(),
		fetcher:      lifecycleFetcher{started: started, allowReturn: allowReturn, err: runErr},
		markets:      types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		mainCtx:      mainCtx,
		cancelMainFn: mainCancel,
		doneCh:       doneCh,
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
	cycleCtx, cycleCancel := context.WithCancel(mainCtx)
	doneCh := make(chan struct{})
	started := make(chan struct{})
	allowReturn := make(chan struct{})
	provider := &Provider{
		logger:        log.NewNopLogger(),
		fetcher:       lifecycleFetcher{started: started, allowReturn: allowReturn, err: errors.New("fetch stopped")},
		markets:       types.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		mainCtx:       mainCtx,
		cancelMainFn:  mainCancel,
		cycleCtx:      cycleCtx,
		cancelCycleFn: cycleCancel,
		doneCh:        doneCh,
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
	if provider.cancelMainFn != nil {
		t.Fatal("main cancel function was not cleared")
	}
	if provider.cycleCtx != nil {
		t.Fatal("cycle context was not cleared")
	}
	if provider.cancelCycleFn != nil {
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

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}
