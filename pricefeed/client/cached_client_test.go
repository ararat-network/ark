package client

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
	apitestutil "github.com/ararat-network/ark/pricefeed/api/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestNewCachedPriceClient(t *testing.T) {
	tests := []struct {
		name    string
		logger  log.Logger
		cfg     Config
		wantErr string
	}{
		{
			name:   "valid",
			logger: log.NewNopLogger(),
			cfg:    validClientConfig(),
		},
		{
			name:    "nil logger",
			cfg:     validClientConfig(),
			wantErr: "logger cannot be nil",
		},
		{
			name:   "disabled does not relax runtime validation",
			logger: log.NewNopLogger(),
			cfg: Config{
				Enabled:        false,
				SidecarAddress: "127.0.0.1:1",
				ClientTimeout:  time.Second,
			},
			wantErr: "price time to live",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.logger, tt.cfg)
			if tt.wantErr != "" {
				require.Nil(t, client)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, client)
		})
	}
}

func TestCachedPriceClientRunReturnsDialError(t *testing.T) {
	cfg := validClientConfig()
	cfg.SidecarAddress = "%"
	client, err := NewClient(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	err = client.Run(context.Background())

	require.ErrorContains(t, err, "dial oracle gRPC server")
	require.ErrorContains(t, err, "invalid URL escape")
}

func TestCachedPriceClientRunRejectsInvalidContext(t *testing.T) {
	client := newTestCachedPriceClient(t, validClientConfig())

	require.EqualError(t, client.Run(nil), "context cannot be nil")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, client.Run(ctx), context.Canceled)
}

func TestCachedPriceClientRunDisabledParksUntilCancelled(t *testing.T) {
	cfg := validClientConfig()
	cfg.Enabled = false
	// The unusable address proves a disabled Run dials nothing.
	cfg.SidecarAddress = "%"
	client, err := NewClient(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() { resultCh <- client.Run(ctx) }()
	cancel()
	require.ErrorIs(t, receiveError(t, resultCh), context.Canceled)
}

func TestCachedPriceClientFetchesImmediately(t *testing.T) {
	response := freshResponse()
	rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
	rpc.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(response, nil)
	client := newTestCachedPriceClient(t, validClientConfig())
	cancel, resultCh := runCachedPricePoller(client, rpc)

	require.Eventually(t, func() bool {
		got, err := client.Prices(context.Background(), &api.PricesRequest{})
		return err == nil && got.Timestamp.Equal(response.Timestamp)
	}, time.Second, time.Millisecond)

	cancel()
	require.ErrorIs(t, receiveError(t, resultCh), context.Canceled)
}

func TestCachedPriceClientRejectsStaleSnapshots(t *testing.T) {
	tests := []struct {
		name      string
		timestamp time.Time
	}{
		{
			name:      "zero timestamp",
			timestamp: time.Time{},
		},
		{
			name:      "expired timestamp",
			timestamp: time.Now().UTC().Add(-time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := freshResponse()
			response.Timestamp = tt.timestamp
			rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
			rpc.EXPECT().
				Prices(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(response, nil)
			client := newTestCachedPriceClient(t, validClientConfig())
			cancel, resultCh := runCachedPricePoller(client, rpc)

			require.Eventually(t, func() bool {
				_, err := client.Prices(context.Background(), &api.PricesRequest{})
				return err != nil && strings.Contains(err.Error(), "too stale")
			}, time.Second, time.Millisecond)

			cancel()
			require.ErrorIs(t, receiveError(t, resultCh), context.Canceled)
		})
	}
}

func TestCachedPriceClientProtectsCachedResponse(t *testing.T) {
	response := freshResponse()
	rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
	rpc.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(response, nil)
	client := newTestCachedPriceClient(t, validClientConfig())
	cancel, resultCh := runCachedPricePoller(client, rpc)

	var first *api.PricesResponse
	require.Eventually(t, func() bool {
		var err error
		first, err = client.Prices(context.Background(), &api.PricesRequest{})
		return err == nil
	}, time.Second, time.Millisecond)

	first.Prices["btc/usd"][0] = 8
	first.Version = "mutated"

	second, err := client.Prices(context.Background(), &api.PricesRequest{})
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, second.Prices["btc/usd"])
	require.Equal(t, "v1.2.3", second.Version)

	cancel()
	require.ErrorIs(t, receiveError(t, resultCh), context.Canceled)
}

func TestCachedPriceClientDoesNotCacheFailedFetch(t *testing.T) {
	tests := []struct {
		name     string
		response *api.PricesResponse
		err      error
	}{
		{
			name: "rpc error",
			err:  status.Error(codes.Unavailable, "sidecar unavailable"),
		},
		{
			name: "nil response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetched := make(chan struct{})
			rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
			rpc.EXPECT().
				Prices(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(context.Context, *api.PricesRequest, ...grpc.CallOption) (*api.PricesResponse, error) {
					close(fetched)
					return tt.response, tt.err
				})
			client := newTestCachedPriceClient(t, validClientConfig())
			cancel, resultCh := runCachedPricePoller(client, rpc)
			requireSignal(t, fetched)

			response, err := client.Prices(context.Background(), &api.PricesRequest{})
			require.Nil(t, response)
			require.EqualError(t, err, "no prices fetched from the sidecar yet")

			cancel()
			require.ErrorIs(t, receiveError(t, resultCh), context.Canceled)
		})
	}
}

func TestCachedPriceClientDoesNotCacheOversizedSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		response func() *api.PricesResponse
	}{
		{
			name: "price count",
			response: func() *api.PricesResponse {
				response := freshResponse()
				response.Prices = make(map[string][]byte, 2*oracletypes.MaxFeeds+1)
				for i := range 2*oracletypes.MaxFeeds + 1 {
					response.Prices[fmt.Sprintf("a%03d", i)] = []byte("1")
				}
				return response
			},
		},
		{
			name: "price bytes",
			response: func() *api.PricesResponse {
				response := freshResponse()
				response.Prices["btc/usd"] = bytes.Repeat(
					[]byte("1"),
					encoding.MaxEncodedLegacyDecBytes+1,
				)
				return response
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
			rpc.EXPECT().
				Prices(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(tt.response(), nil)
			client := newTestCachedPriceClient(t, validClientConfig())

			client.fetchPrices(context.Background(), rpc)

			response, err := client.Prices(context.Background(), &api.PricesRequest{})
			require.Nil(t, response)
			require.EqualError(t, err, "no prices fetched from the sidecar yet")
		})
	}
}

func TestCachedPriceClientPricesHonoursContext(t *testing.T) {
	client := newTestCachedPriceClient(t, validClientConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	response, err := client.Prices(ctx, &api.PricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCachedPriceClientPollCancellationCancelsBlockedFetch(t *testing.T) {
	entered := make(chan struct{})
	rpc := blockingPriceClient(t, entered)
	client := newTestCachedPriceClient(t, validClientConfig())
	cancel, resultCh := runCachedPricePoller(client, rpc)
	requireSignal(t, entered)

	cancel()
	require.ErrorIs(t, receiveError(t, resultCh), context.Canceled)
}

func blockingPriceClient(t *testing.T, entered chan<- struct{}) *apitestutil.MockPriceFeedClient {
	t.Helper()

	var enteredOnce sync.Once
	rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
	rpc.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *api.PricesRequest, _ ...grpc.CallOption) (*api.PricesResponse, error) {
			enteredOnce.Do(func() { close(entered) })
			<-ctx.Done()
			return nil, status.FromContextError(ctx.Err()).Err()
		})

	return rpc
}

func newTestCachedPriceClient(
	t *testing.T,
	cfg Config,
) *Client {
	t.Helper()

	client, err := NewClient(log.NewTestLogger(t), cfg)
	require.NoError(t, err)
	return client
}

func runCachedPricePoller(
	client *Client,
	rpc api.PriceFeedClient,
) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- client.poll(ctx, rpc)
	}()
	return cancel, resultCh
}

func requireSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for signal")
	}
}

func receiveError(t *testing.T, ch <-chan error) error {
	t.Helper()

	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		require.FailNow(t, "timed out waiting for result")
		return nil
	}
}

func freshResponse() *api.PricesResponse {
	return &api.PricesResponse{
		Prices: map[string][]byte{
			"btc/usd": {1, 2, 3},
		},
		Timestamp: time.Now().UTC(),
		Version:   "v1.2.3",
	}
}

func validClientConfig() Config {
	return Config{
		Enabled:        true,
		SidecarAddress: "127.0.0.1:1",
		ClientTimeout:  time.Second,
		Interval:       5 * time.Second,
		PriceTTL:       10 * time.Second,
	}
}
