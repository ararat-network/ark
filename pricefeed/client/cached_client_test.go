package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
				Enabled:          false,
				SidecarAddresses: []string{"127.0.0.1:1"},
				ClientTimeout:    time.Second,
			},
			wantErr: "price_ttl",
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

func TestCachedPriceClientOwnsAddresses(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			cfg := validClientConfig()
			cfg.Enabled = enabled
			cfg.SidecarAddresses = []string{"127.0.0.1:8080", "127.0.0.2:8080"}
			client := newTestCachedPriceClient(t, cfg)

			cfg.SidecarAddresses[0] = "remote.example:8080"
			require.Equal(t, []string{"127.0.0.1:8080", "127.0.0.2:8080"}, client.config.SidecarAddresses)
		})
	}
}

func TestCachedPriceClientRunReturnsDialError(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
	}{
		{
			name:      "only address",
			addresses: []string{"%"},
		},
		{
			name:      "later address",
			addresses: []string{"127.0.0.1:1", "%"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validClientConfig()
			cfg.SidecarAddresses = tt.addresses
			cfg.TLS.Mode = "plaintext"
			client, err := NewClient(log.NewNopLogger(), cfg)
			require.NoError(t, err)

			err = client.Run(context.Background())

			require.ErrorContains(t, err, "dial sidecars: open connection to %")
			require.ErrorContains(t, err, "invalid URL escape")
		})
	}
}

func TestCachedPriceClientRunRejectsInvalidContext(t *testing.T) {
	client := newTestCachedPriceClient(t, validClientConfig())

	require.EqualError(t, client.Run(nil), "context cannot be nil") //nolint:staticcheck // the nil context is the case under test

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, client.Run(ctx), context.Canceled)
}

func TestCachedPriceClientRunDisabledParksUntilCancelled(t *testing.T) {
	cfg := validClientConfig()
	cfg.Enabled = false
	// The unusable address proves a disabled Run dials nothing.
	cfg.SidecarAddresses = []string{"%"}
	cfg.TLS.Mode = "plaintext"
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
	cancel, resultCh := runCachedPricePoller(client, testEndpoints(rpc))

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
			client := newTestCachedPriceClient(t, validClientConfig())
			client.resp = response

			_, err := client.Prices(context.Background(), &api.PricesRequest{})
			require.ErrorContains(t, err, "too stale")
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
	cancel, resultCh := runCachedPricePoller(client, testEndpoints(rpc))

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
			cancel, resultCh := runCachedPricePoller(client, testEndpoints(rpc))
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
			name:     "price count",
			response: oversizedCountResponse,
		},
		{
			name:     "price bytes",
			response: oversizedPriceResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := apitestutil.NewMockPriceFeedClient(gomock.NewController(t))
			rpc.EXPECT().
				Prices(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(tt.response(), nil)
			client := newTestCachedPriceClient(t, validClientConfig())

			client.fetchPrices(context.Background(), testEndpoints(rpc))

			response, err := client.Prices(context.Background(), &api.PricesRequest{})
			require.Nil(t, response)
			require.EqualError(t, err, "no prices fetched from the sidecar yet")
		})
	}
}

// Whatever fails the preferred sidecar, the next in order is tried within the
// same fetch, and the one that answers becomes active.
func TestCachedPriceClientFailsOverInOrder(t *testing.T) {
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
		{
			name:     "oversized snapshot",
			response: oversizedPriceResponse(),
		},
		{
			name:     "zero timestamp",
			response: &api.PricesResponse{},
		},
		{
			name:     "expired timestamp",
			response: &api.PricesResponse{Timestamp: time.Now().Add(-time.Minute)},
		},
		{
			name:     "timestamp ahead of clock",
			response: &api.PricesResponse{Timestamp: time.Now().Add(time.Minute)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := freshResponse()
			ctrl := gomock.NewController(t)
			primary := apitestutil.NewMockPriceFeedClient(ctrl)
			secondary := apitestutil.NewMockPriceFeedClient(ctrl)
			gomock.InOrder(
				primary.EXPECT().
					Prices(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(tt.response, tt.err),
				secondary.EXPECT().
					Prices(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(response, nil),
			)
			client := newTestCachedPriceClient(t, validClientConfig())

			client.fetchPrices(context.Background(), testEndpoints(primary, secondary))

			got, err := client.Prices(context.Background(), &api.PricesRequest{})
			require.NoError(t, err)
			require.True(t, got.Timestamp.Equal(response.Timestamp))
			require.Equal(t, 1, client.active)
		})
	}
}

// Once a sidecar has served, the next fetch starts there. The preferred one is
// not retried while the active one answers, so a flapping preferred sidecar
// cannot bounce the client back and forth.
func TestCachedPriceClientStaysOnServingSidecar(t *testing.T) {
	ctrl := gomock.NewController(t)
	primary := apitestutil.NewMockPriceFeedClient(ctrl)
	secondary := apitestutil.NewMockPriceFeedClient(ctrl)
	primary.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.Unavailable, "sidecar unavailable")).
		Times(1)
	secondary.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(freshResponse(), nil).
		Times(2)
	client := newTestCachedPriceClient(t, validClientConfig())
	endpoints := testEndpoints(primary, secondary)

	client.fetchPrices(context.Background(), endpoints)
	client.fetchPrices(context.Background(), endpoints)

	require.Equal(t, 1, client.active)
}

// The sweep wraps from the active sidecar to the head of the list and stops
// at the first that answers; the mock without expectations proves the one
// after it is never reached.
func TestCachedPriceClientSweepWrapsAround(t *testing.T) {
	ctrl := gomock.NewController(t)
	first := apitestutil.NewMockPriceFeedClient(ctrl)
	second := apitestutil.NewMockPriceFeedClient(ctrl)
	third := apitestutil.NewMockPriceFeedClient(ctrl)
	gomock.InOrder(
		third.EXPECT().
			Prices(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, status.Error(codes.Unavailable, "sidecar unavailable")),
		first.EXPECT().
			Prices(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(freshResponse(), nil),
	)
	client := newTestCachedPriceClient(t, validClientConfig())
	client.active = 2

	client.fetchPrices(context.Background(), testEndpoints(first, second, third))

	require.Equal(t, 0, client.active)
}

func TestCachedPriceClientDoesNotCacheWhenEverySidecarFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	primary := apitestutil.NewMockPriceFeedClient(ctrl)
	secondary := apitestutil.NewMockPriceFeedClient(ctrl)
	primary.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, status.Error(codes.Unavailable, "sidecar unavailable"))
	secondary.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errors.New("sidecar broken"))
	client := newTestCachedPriceClient(t, validClientConfig())

	client.fetchPrices(context.Background(), testEndpoints(primary, secondary))

	response, err := client.Prices(context.Background(), &api.PricesRequest{})
	require.Nil(t, response)
	require.EqualError(t, err, "no prices fetched from the sidecar yet")
	require.Equal(t, 0, client.active)
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
	cancel, resultCh := runCachedPricePoller(client, testEndpoints(rpc))
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
	endpoints []endpoint,
) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- client.poll(ctx, endpoints)
	}()
	return cancel, resultCh
}

// testEndpoints pairs each mock with a distinct address, in list order.
func testEndpoints(rpcs ...api.PriceFeedClient) []endpoint {
	endpoints := make([]endpoint, len(rpcs))
	for i, rpc := range rpcs {
		endpoints[i] = endpoint{address: fmt.Sprintf("sidecar-%d", i), rpc: rpc}
	}
	return endpoints
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

func oversizedCountResponse() *api.PricesResponse {
	response := freshResponse()
	response.Prices = make(map[string][]byte, 2*oracletypes.MaxFeeds+1)
	for i := range 2*oracletypes.MaxFeeds + 1 {
		response.Prices[fmt.Sprintf("a%03d", i)] = []byte("1")
	}
	return response
}

func oversizedPriceResponse() *api.PricesResponse {
	response := freshResponse()
	response.Prices["btc/usd"] = bytes.Repeat(
		[]byte("1"),
		encoding.MaxEncodedCompactLegacyDecBytes+1,
	)
	return response
}

func validClientConfig() Config {
	return Config{
		Enabled:          true,
		SidecarAddresses: []string{"127.0.0.1:1"},
		ClientTimeout:    time.Second,
		Interval:         5 * time.Second,
		PriceTTL:         10 * time.Second,
	}
}

// Cancellation ends the sweep: the mock without expectations proves the next
// sidecar is not tried once an attempt has seen it.
func TestCachedPriceClientSweepStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctrl := gomock.NewController(t)
	primary := apitestutil.NewMockPriceFeedClient(ctrl)
	secondary := apitestutil.NewMockPriceFeedClient(ctrl)
	primary.EXPECT().
		Prices(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, *api.PricesRequest, ...grpc.CallOption) (*api.PricesResponse, error) {
			cancel()
			return nil, status.Error(codes.Unavailable, "sidecar unavailable")
		})
	client := newTestCachedPriceClient(t, validClientConfig())

	client.fetchPrices(ctx, testEndpoints(primary, secondary))

	_, err := client.Prices(context.Background(), &api.PricesRequest{})
	require.EqualError(t, err, "no prices fetched from the sidecar yet")
}
