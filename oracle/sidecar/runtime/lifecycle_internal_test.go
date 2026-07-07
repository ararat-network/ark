package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers/base"
	basetestutil "noah/oracle/sidecar/providers/base/testutil"
	providertypes "noah/oracle/sidecar/providers/types"
)

func TestStartProvidersWaitsForRuntimeUpdate(t *testing.T) {
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

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.Fail(t, message)
	}
}
