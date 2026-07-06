package chainstate

import (
	"context"
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
