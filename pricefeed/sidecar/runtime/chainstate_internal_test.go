package runtime

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	runtimetestutil "github.com/ararat-network/ark/pricefeed/sidecar/runtime/testutil"
)

func TestRunReturnsChainStateClientError(t *testing.T) {
	logs := new(bytes.Buffer)
	runErr := errors.New("client failed")
	ctrl := gomock.NewController(t)
	client := runtimetestutil.NewMockChainStateClient(ctrl)
	client.EXPECT().Run(gomock.Any()).Return(runErr)
	oracle := newClientLifecycleRuntime(client, log.NewLogger(logs, log.ColorOption(false)))

	err := oracle.Run(context.Background())

	require.ErrorIs(t, err, runErr)
	require.Contains(t, logs.String(), "chain state client exited")
	require.Contains(t, logs.String(), "client failed")
}

func TestRunRejectsUnexpectedChainStateClientExit(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := runtimetestutil.NewMockChainStateClient(ctrl)
	client.EXPECT().Run(gomock.Any()).Return(nil)
	oracle := newClientLifecycleRuntime(client, log.NewNopLogger())

	err := oracle.Run(context.Background())

	require.ErrorIs(t, err, errChainStateClientExited)
}

func TestRunDoesNotLogIntentionalChainStateClientCancellation(t *testing.T) {
	logs := new(bytes.Buffer)
	started := make(chan struct{})
	ctrl := gomock.NewController(t)
	client := runtimetestutil.NewMockChainStateClient(ctrl)
	client.EXPECT().
		Run(gomock.Any()).
		DoAndReturn(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	oracle := newClientLifecycleRuntime(client, log.NewLogger(logs, log.ColorOption(false)))
	require.False(t, oracle.IsRunning())

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Run(ctx)
	}()
	requireSignal(t, started, "chain state client did not start")
	// Run records itself as running only after starting the client.
	require.Eventually(t, oracle.IsRunning, time.Second, time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop")
	}
	require.False(t, oracle.IsRunning())
	require.NotContains(t, logs.String(), "chain state client exited")
}

func newClientLifecycleRuntime(client ChainStateClient, logger log.Logger) *Runtime {
	return &Runtime{
		logger:    logger,
		client:    client,
		providers: make(map[string]*managedProvider),
		cfg: Config{
			UpdateInterval: time.Hour,
		},
	}
}
