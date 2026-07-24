package chainstate

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	oracletypes "ark/x/oracle/types"
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
			panic("vote target query panic")
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
		require.Equal(t, "vote target query panic", recovered)
	case <-time.After(time.Second):
		cancel()
		t.Fatal("chain state client did not propagate panic")
	}
}

func TestQueryVoteTargetsRejectsOversizedEpochs(t *testing.T) {
	client, err := NewClient(Config{
		Address:  "passthrough:///unused",
		Timeout:  time.Second,
		Interval: time.Second,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		response *oracletypes.QueryVoteTargetsResponse
		wantErr  string
	}{
		{
			name: "active",
			response: &oracletypes.QueryVoteTargetsResponse{
				VoteTargets: make([]string, oracletypes.MaxVoteTargets+1),
			},
			wantErr: "active vote target count",
		},
		{
			name: "pending",
			response: &oracletypes.QueryVoteTargetsResponse{
				Pending: &oracletypes.PendingVoteTargets{
					Denoms: make([]string, oracletypes.MaxVoteTargets+1),
				},
			},
			wantErr: "pending vote target count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := voteTargetQueryClient{response: tt.response}

			_, err := client.queryVoteTargets(context.Background(), query)

			require.ErrorContains(t, err, tt.wantErr)
			require.ErrorContains(t, err, "exceeds maximum")
		})
	}
}

type voteTargetQueryClient struct {
	oracletypes.QueryClient
	response *oracletypes.QueryVoteTargetsResponse
}

func (c voteTargetQueryClient) VoteTargets(
	context.Context,
	*oracletypes.QueryVoteTargetsRequest,
	...grpc.CallOption,
) (*oracletypes.QueryVoteTargetsResponse, error) {
	return c.response, nil
}
