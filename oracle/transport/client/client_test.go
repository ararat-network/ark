package client_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	oracleclient "noah/oracle/transport/client"
	"noah/oracle/transport/types"
)

type testOracleServer struct {
	types.UnimplementedOracleServer

	prices   *types.OraclePricesResponse
	version  *types.OracleVersionResponse
	block    bool
	pricesFn func(context.Context) (*types.OraclePricesResponse, error)
}

func (s testOracleServer) Prices(
	ctx context.Context,
	_ *types.OraclePricesRequest,
) (*types.OraclePricesResponse, error) {
	if s.pricesFn != nil {
		return s.pricesFn(ctx)
	}
	if s.block {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	return s.prices, nil
}

func (s testOracleServer) Version(
	_ context.Context,
	_ *types.OracleVersionRequest,
) (*types.OracleVersionResponse, error) {
	return s.version, nil
}

func TestNewClient(t *testing.T) {
	testCases := []struct {
		name    string
		logger  log.Logger
		timeout time.Duration
		wantErr string
	}{
		{
			name:    "valid",
			logger:  log.NewNopLogger(),
			timeout: time.Second,
		},
		{
			name:    "nil logger",
			timeout: time.Second,
			wantErr: "logger cannot be nil",
		},
		{
			name:    "zero timeout",
			logger:  log.NewNopLogger(),
			timeout: 0,
			wantErr: "timeout must be positive",
		},
		{
			name:    "negative timeout",
			logger:  log.NewNopLogger(),
			timeout: -time.Second,
			wantErr: "timeout must be positive",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := oracleclient.NewClient(tc.logger, "127.0.0.1:1", tc.timeout)
			if tc.wantErr != "" {
				require.Nil(t, client)
				require.EqualError(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.IsType(t, &oracleclient.Client{}, client)
		})
	}
}

func TestClientReturnsErrorBeforeStart(t *testing.T) {
	client, err := oracleclient.NewClient(log.NewNopLogger(), "127.0.0.1:1", time.Second)
	require.NoError(t, err)

	t.Run("prices", func(t *testing.T) {
		response, err := client.Prices(context.Background(), &types.OraclePricesRequest{})

		require.Nil(t, response)
		require.EqualError(t, err, "oracle client not started")
	})

	t.Run("version", func(t *testing.T) {
		response, err := client.Version(context.Background(), &types.OracleVersionRequest{})

		require.Nil(t, response)
		require.EqualError(t, err, "oracle client not started")
	})
}

func TestClientStopBeforeStart(t *testing.T) {
	client, err := oracleclient.NewClient(log.NewNopLogger(), "127.0.0.1:1", time.Second)
	require.NoError(t, err)

	require.NoError(t, client.Stop())
}

func TestClientRPCs(t *testing.T) {
	expectedPrices := &types.OraclePricesResponse{
		Prices: map[string][]byte{
			"BTC/USD": {1, 2, 3},
		},
		Version: "v1.2.3",
	}
	expectedVersion := &types.OracleVersionResponse{Version: "v1.2.3"}
	addr := startTestOracleServer(t, testOracleServer{
		prices:  expectedPrices,
		version: expectedVersion,
	})

	client, err := oracleclient.NewClient(log.NewNopLogger(), addr, time.Second)
	require.NoError(t, err)
	require.NoError(t, client.Start(context.Background()))
	t.Cleanup(func() {
		require.NoError(t, client.Stop())
	})

	prices, err := client.Prices(context.Background(), &types.OraclePricesRequest{})
	require.NoError(t, err)
	require.Equal(t, expectedPrices, prices)

	version, err := client.Version(context.Background(), &types.OracleVersionRequest{})
	require.NoError(t, err)
	require.Equal(t, expectedVersion, version)
}

func TestClientPricesTimeout(t *testing.T) {
	addr := startTestOracleServer(t, testOracleServer{block: true})
	client, err := oracleclient.NewClient(log.NewNopLogger(), addr, 20*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, client.Start(context.Background()))
	t.Cleanup(func() {
		require.NoError(t, client.Stop())
	})

	response, err := client.Prices(context.Background(), &types.OraclePricesRequest{})

	require.Nil(t, response)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func startTestOracleServer(t *testing.T, service types.OracleServer) string {
	t.Helper()

	socketPath := filepath.Join(
		"/private/tmp",
		"noah-oracle-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock",
	)
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)

	server := grpc.NewServer()
	types.RegisterOracleServer(server, service)
	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Stop()
		_ = os.Remove(socketPath)
	})

	return "unix://" + socketPath
}
