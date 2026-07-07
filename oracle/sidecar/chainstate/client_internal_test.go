package chainstate

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestStopClosesUpdateChannel(t *testing.T) {
	dialer := func(context.Context, string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		t.Cleanup(func() { _ = serverConn.Close() })
		return clientConn, nil
	}

	client, err := NewClient(
		Config{
			Address:  "passthrough:///unused",
			Timeout:  time.Hour,
			Interval: time.Hour,
		},
		WithDialOptions(grpc.WithContextDialer(dialer)),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, client.Start(ctx))

	require.Eventually(t, func() bool {
		client.mut.RLock()
		defer client.mut.RUnlock()
		return client.updateCh != nil
	}, time.Second, time.Millisecond)

	client.mut.RLock()
	updateCh := client.updateCh
	client.mut.RUnlock()

	client.Stop()

	select {
	case _, ok := <-updateCh:
		require.False(t, ok)
	default:
		t.Fatal("update channel should be closed on stop")
	}
}

func TestRunRepanicsPollPanicAfterLifecycleCleanup(t *testing.T) {
	client, err := NewClient(
		Config{
			Address:  "passthrough:///unused",
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		WithDialOptions(grpc.WithUnaryInterceptor(func(
			context.Context,
			string,
			any,
			any,
			*grpc.ClientConn,
			grpc.UnaryInvoker,
			...grpc.CallOption,
		) error {
			panic("vote target query panic")
		})),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	updateCh := make(chan struct{}, 1)
	doneCh := make(chan struct{})
	recoveredCh := make(chan any, 1)
	go func() {
		defer func() {
			recoveredCh <- recover()
		}()
		client.run(ctx, cancel, updateCh, doneCh)
	}()

	select {
	case recovered := <-recoveredCh:
		require.ErrorContains(t, recovered.(error), "chain state client panicked")
		require.Contains(t, fmt.Sprint(recovered), "vote target query panic")
	case <-time.After(time.Second):
		cancel()
		t.Fatal("chain state client did not re-panic")
	}

	select {
	case <-doneCh:
	default:
		t.Fatal("done channel was not closed")
	}
	select {
	case _, ok := <-updateCh:
		require.False(t, ok)
	default:
		t.Fatal("update channel was not closed")
	}
}
