package chainstate

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ararat-network/ark/pkg/tlsconfig"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func withDialOptions(opts ...grpc.DialOption) Option {
	return func(c *Client) {
		c.dialOptions = append(c.dialOptions, opts...)
	}
}

func TestClientOwnsAddresses(t *testing.T) {
	for _, update := range []bool{false, true} {
		name := "construction"
		if update {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			cfg := Config{
				Addresses: []string{"127.0.0.1:9090", "127.0.0.2:9090"},
				Timeout:   time.Second,
				Interval:  time.Second,
			}
			client, err := NewClient(cfg)
			require.NoError(t, err)
			if update {
				cfg.Addresses = []string{"127.0.0.3:9090", "127.0.0.4:9090"}
				require.NoError(t, client.Update(cfg))
			}
			want := append([]string(nil), cfg.Addresses...)

			cfg.Addresses[0] = "remote.example:9090"
			require.Equal(t, want, client.getConfig().Addresses)
		})
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
			TLS:       tlsconfig.Client{Mode: tlsconfig.Plaintext},
			Addresses: []string{"passthrough:///unused"},
			Timeout:   time.Hour,
			Interval:  time.Hour,
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
			TLS:       tlsconfig.Client{Mode: tlsconfig.Plaintext},
			Addresses: []string{"passthrough:///unused"},
			Timeout:   time.Second,
			Interval:  time.Hour,
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
		TLS:       tlsconfig.Client{Mode: tlsconfig.Plaintext},
		Addresses: []string{"passthrough:///unused"},
		Timeout:   time.Second,
		Interval:  time.Second,
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

// waitForRetry is the only place the polling loop yields to cancellation
// between attempts. Its two outcomes are what decide whether a shutdown waits
// out a full retry interval or returns at once.
func TestWaitForRetryReturnsAfterTheInterval(t *testing.T) {
	require.NoError(t, waitForRetry(context.Background(), time.Millisecond))
}

func TestWaitForRetryReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, waitForRetry(ctx, time.Hour), context.Canceled)
}

// Cancellation ends the sweep: the second node is not tried once the first
// attempt has seen it. The interceptor answers before any connection is made.
func TestRefreshStopsSweepOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var targets []string
	client, err := NewClient(
		Config{
			TLS:       tlsconfig.Client{Mode: tlsconfig.Plaintext},
			Addresses: []string{"passthrough:///first", "passthrough:///second"},
			Timeout:   time.Second,
			Interval:  time.Hour,
		},
		withDialOptions(grpc.WithUnaryInterceptor(func(
			_ context.Context,
			_ string,
			_, _ any,
			cc *grpc.ClientConn,
			_ grpc.UnaryInvoker,
			_ ...grpc.CallOption,
		) error {
			targets = append(targets, cc.Target())
			cancel()
			return status.Error(codes.Unavailable, "node unavailable")
		})),
	)
	require.NoError(t, err)

	err = client.Run(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"passthrough:///first"}, targets)
}
