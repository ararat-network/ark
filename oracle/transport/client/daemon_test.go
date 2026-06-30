package client_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	oracleclient "noah/oracle/transport/client"
	"noah/oracle/transport/types"
)

func TestNewPriceDaemon(t *testing.T) {
	tests := []struct {
		name    string
		logger  log.Logger
		cfg     oracleclient.Config
		wantErr bool
	}{
		{
			name:   "valid",
			logger: log.NewNopLogger(),
			cfg:    validDaemonConfig(),
		},
		{
			name:    "nil logger",
			cfg:     validDaemonConfig(),
			wantErr: true,
		},
		{
			name:   "invalid config",
			logger: log.NewNopLogger(),
			cfg: oracleclient.Config{
				Enabled: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon, err := oracleclient.NewPriceDaemon(tt.logger, tt.cfg)
			if tt.wantErr {
				require.Nil(t, daemon)
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, daemon)
		})
	}
}

func TestPriceDaemonCachesPrices(t *testing.T) {
	prices := map[string][]byte{
		"btc/usd": []byte("10000"),
	}
	addr := startTestOracleServer(t, testOracleServer{
		prices: &types.OraclePricesResponse{Prices: prices},
	})
	cfg := validDaemonConfig()
	cfg.OracleAddress = addr
	cfg.Interval = 20 * time.Millisecond

	daemon, err := oracleclient.NewPriceDaemon(log.NewTestLogger(t), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- daemon.Start(ctx)
	}()

	require.Eventually(t, func() bool {
		response, err := daemon.Prices(context.Background(), &types.OraclePricesRequest{})
		return err == nil && response != nil
	}, time.Second, 10*time.Millisecond)

	response, err := daemon.Prices(context.Background(), &types.OraclePricesRequest{})
	require.NoError(t, err)
	require.Equal(t, prices, response.Prices)

	cancel()
	require.ErrorIs(t, <-resultCh, context.Canceled)
}

func TestPriceDaemonRejectsStalePrices(t *testing.T) {
	prices := map[string][]byte{
		"btc/usd": []byte("10000"),
	}
	var calls atomic.Int64
	addr := startTestOracleServer(t, testOracleServer{
		pricesFn: func(context.Context) (*types.OraclePricesResponse, error) {
			if calls.Add(1) == 1 {
				return &types.OraclePricesResponse{Prices: prices}, nil
			}
			return nil, status.Error(codes.Unavailable, "failed to make request")
		},
	})
	cfg := validDaemonConfig()
	cfg.OracleAddress = addr
	cfg.Interval = 20 * time.Millisecond
	cfg.PriceTTL = 50 * time.Millisecond

	daemon, err := oracleclient.NewPriceDaemon(log.NewTestLogger(t), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- daemon.Start(ctx)
	}()

	require.Eventually(t, func() bool {
		_, err := daemon.Prices(context.Background(), &types.OraclePricesRequest{})
		return err == nil
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		_, err := daemon.Prices(context.Background(), &types.OraclePricesRequest{})
		return err != nil
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.ErrorIs(t, <-resultCh, context.Canceled)
}

func TestPriceDaemonPricesBeforeStart(t *testing.T) {
	daemon, err := oracleclient.NewPriceDaemon(log.NewNopLogger(), validDaemonConfig())
	require.NoError(t, err)

	response, err := daemon.Prices(context.Background(), &types.OraclePricesRequest{})

	require.Nil(t, response)
	require.EqualError(t, err, "no prices fetched by price daemon yet")
}

func TestPriceDaemonStop(t *testing.T) {
	addr := startTestOracleServer(t, testOracleServer{
		prices: &types.OraclePricesResponse{},
	})
	cfg := validDaemonConfig()
	cfg.OracleAddress = addr
	cfg.Interval = 20 * time.Millisecond

	daemon, err := oracleclient.NewPriceDaemon(log.NewTestLogger(t), cfg)
	require.NoError(t, err)

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- daemon.Start(context.Background())
	}()

	require.Eventually(t, func() bool {
		_, err := daemon.Prices(context.Background(), &types.OraclePricesRequest{})
		return err == nil
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, daemon.Stop())
	require.NoError(t, <-resultCh)
}

func TestPriceDaemonDoesNotCacheClientErrors(t *testing.T) {
	addr := startTestOracleServer(t, testOracleServer{
		pricesFn: func(context.Context) (*types.OraclePricesResponse, error) {
			return nil, errors.New("failed to make request")
		},
	})
	cfg := validDaemonConfig()
	cfg.OracleAddress = addr
	cfg.Interval = 20 * time.Millisecond

	daemon, err := oracleclient.NewPriceDaemon(log.NewTestLogger(t), cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, daemon.Start(ctx), context.DeadlineExceeded)
	response, err := daemon.Prices(context.Background(), &types.OraclePricesRequest{})
	require.Nil(t, response)
	require.EqualError(t, err, "no prices fetched by price daemon yet")
}

func validDaemonConfig() oracleclient.Config {
	return oracleclient.Config{
		Enabled:       true,
		OracleAddress: "127.0.0.1:1",
		ClientTimeout: time.Second,
		Interval:      time.Second,
		PriceTTL:      2 * time.Second,
	}
}
