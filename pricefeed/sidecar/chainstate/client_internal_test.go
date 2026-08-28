package chainstate

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func withDialOptions(opts ...grpc.DialOption) Option {
	return func(c *Client) {
		c.dialOptions = append(c.dialOptions, opts...)
	}
}

func TestRunReturnsContextCancellation(t *testing.T) {
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
		withDialOptions(grpc.WithContextDialer(dialer)),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx)
	}()

	cancel()
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("chain state client did not stop")
	}
}

func TestRunPropagatesPollPanic(t *testing.T) {
	allowPanic := make(chan struct{})
	client, err := NewClient(
		Config{
			Address:  "passthrough:///unused",
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		withDialOptions(grpc.WithUnaryInterceptor(func(
			context.Context,
			string,
			any,
			any,
			*grpc.ClientConn,
			grpc.UnaryInvoker,
			...grpc.CallOption,
		) error {
			<-allowPanic
			panic("feed query panic")
		})),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recoveredCh := make(chan any, 1)
	go func() {
		defer func() {
			recoveredCh <- recover()
		}()
		_ = client.Run(ctx)
	}()

	close(allowPanic)

	select {
	case recovered := <-recoveredCh:
		require.Equal(t, "feed query panic", recovered)
	case <-time.After(time.Second):
		cancel()
		t.Fatal("chain state client did not propagate panic")
	}
}

func TestQueryFeedsRejectsOversizedEpochs(t *testing.T) {
	client, err := NewClient(Config{
		Address:  "passthrough:///unused",
		Timeout:  time.Second,
		Interval: time.Second,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		response *oracletypes.QueryFeedsResponse
		wantErr  string
	}{
		{
			name: "active",
			response: &oracletypes.QueryFeedsResponse{
				Feeds: oracletypes.Feeds{
					Denoms: make([]string, oracletypes.MaxFeeds+1),
				},
			},
			wantErr: "active feed count",
		},
		{
			name: "scheduled transitions",
			response: &oracletypes.QueryFeedsResponse{
				Feeds: oracletypes.Feeds{
					Transitions: make([]oracletypes.FeedTransition, oracletypes.MaxFeeds+1),
				},
			},
			wantErr: "scheduled feed transition count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := feedQueryClient{response: tt.response}

			_, err := client.queryFeeds(context.Background(), query)

			require.ErrorContains(t, err, tt.wantErr)
			require.ErrorContains(t, err, "exceeds maximum")
		})
	}
}

type feedQueryClient struct {
	oracletypes.QueryClient
	response *oracletypes.QueryFeedsResponse
}

func (c feedQueryClient) Feeds(
	context.Context,
	*oracletypes.QueryFeedsRequest,
	...grpc.CallOption,
) (*oracletypes.QueryFeedsResponse, error) {
	return c.response, nil
}
