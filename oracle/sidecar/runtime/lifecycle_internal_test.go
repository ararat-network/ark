package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers/base"
	providertypes "noah/oracle/sidecar/providers/types"
)

func TestStartProvidersWaitsForRuntimeUpdate(t *testing.T) {
	started := make(chan struct{})
	fetcher := &lifecycleTestFetcher{started: started}
	provider, err := base.NewProvider(
		"test",
		base.API,
		providertypes.Markets{{Pair: "ATOM/USD", Symbol: "ATOMUSD"}},
		fetcher,
		base.WithLogger(log.NewNopLogger()),
	)
	require.NoError(t, err)

	oracle := &Runtime{
		logger: log.NewNopLogger(),
		providers: map[string]*base.Provider{
			provider.Name(): provider,
		},
	}

	oracle.updateMu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDone := make(chan struct{})
	go func() {
		oracle.startProviders(ctx)
		close(startDone)
	}()

	select {
	case <-startDone:
		t.Fatal("startProviders returned while runtime update lock was held")
	case <-time.After(20 * time.Millisecond):
	}

	oracle.updateMu.Unlock()

	requireSignal(t, startDone, "startProviders did not return")
	requireSignal(t, started, "provider did not start")

	provider.Stop()
}

type lifecycleTestFetcher struct {
	started chan<- struct{}
	once    sync.Once
}

func (f *lifecycleTestFetcher) Run(
	ctx context.Context,
	_ []providertypes.Ticker,
	_ chan<- providertypes.Response,
) error {
	f.once.Do(func() {
		close(f.started)
	})
	<-ctx.Done()
	return ctx.Err()
}

func (f *lifecycleTestFetcher) Type() base.TransportType { return base.API }

func (f *lifecycleTestFetcher) Name() string { return "test" }

func (f *lifecycleTestFetcher) ResponseBufferSize([]providertypes.Ticker) int { return 1 }

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.Fail(t, message)
	}
}
