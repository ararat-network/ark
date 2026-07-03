package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	"noah/oracle/types"
)

type CachedClient struct {
	logger log.Logger

	// isRunning is an atomic boolean that indicates whether the daemon is running.
	isRunning atomic.Bool
	// config is the configuration of the daemon.
	config Config
	// client is the underlying oracle client used to fetch prices.
	client *Client
	// latestResponse is the latest price response fetched by the daemon.
	resp ThreadSafeResponse
	// doneCh is a channel that is closed when the daemon is stopped.
	doneCh chan struct{}
}

// NewCachedClient creates a cached gRPC client.
func NewCachedClient(
	logger log.Logger,
	cfg Config,
	opts ...Option,
) (*CachedClient, error) {
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client, err := NewClient(
		logger.With("client", "oracle"),
		cfg.OracleAddress,
		cfg.ClientTimeout,
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return &CachedClient{
		logger: logger.With("process", "price_daemon"),
		config: cfg,
		client: client,
		doneCh: make(chan struct{}),
	}, nil
}

// Start connects the underlying gRPC client and runs the price polling loop.
// This method blocks until the daemon is stopped or the context is cancelled.
func (d *CachedClient) Start(ctx context.Context) (err error) {
	if err := d.client.Start(ctx); err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, d.client.Stop())
	}()

	ticker := time.NewTicker(d.config.Interval)
	defer ticker.Stop()

	d.logger.Info("starting price daemon")
	d.isRunning.Store(true)
	defer d.isRunning.Store(false)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("stopping price daemon from context")
			return ctx.Err()
		case <-d.doneCh:
			d.logger.Info("price daemon stopped")
			return nil
		case <-ticker.C:
			d.fetchPrices(ctx)
		}
	}
}

// fetchPrices fetches the latest prices from the oracle client.
func (d *CachedClient) fetchPrices(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("recovered from panic", "err", r)
		}
	}()

	d.logger.Debug("fetching prices")

	fetchCtx, cancel := context.WithTimeout(ctx, d.config.ClientTimeout)
	defer cancel()

	resp, err := d.client.Prices(fetchCtx, &types.OraclePricesRequest{})
	if err != nil {
		d.logger.Error(
			"failed to fetch prices from sidecar",
			"err", err,
			"address", d.config.OracleAddress,
		)

		return
	}

	ts := time.Now().UTC()
	d.logger.Debug("fetched prices", "timestamp", ts, "prices", resp.Prices)
	d.resp.Update(resp)
}

// Prices returns the latest price response fetched by the daemon. If the latest response
// is too stale, an error is returned.
func (d *CachedClient) Prices(
	_ context.Context,
	_ *types.OraclePricesRequest,
	_ ...grpc.CallOption,
) (*types.OraclePricesResponse, error) {
	latest, ts := d.resp.Get()
	if latest == nil {
		d.logger.Error("no prices fetched by price daemon yet")
		return nil, errors.New("no prices fetched by price daemon yet")
	}

	if time.Since(ts) > d.config.PriceTTL {
		d.logger.Error(
			"latest prices from the price daemon are too stale",
			"last_fetched_at", ts.String(),
			"diff", time.Since(ts).String(),
			"ttl", d.config.PriceTTL.String(),
		)

		return nil, fmt.Errorf(
			"latest prices from the price daemon are too stale; last fetched at %s; diff %s ago",
			ts.Format(time.RFC3339),
			time.Since(ts).String(),
		)
	}

	return latest, nil
}

// Stop stops the price daemon.
func (d *CachedClient) Stop() error {
	if d.isRunning.Load() {
		d.doneCh <- struct{}{}
		close(d.doneCh)
	}

	return nil
}

// ThreadSafeResponse is a thread-safe wrapper around a QueryPricesResponse.
type ThreadSafeResponse struct {
	sync.Mutex

	resp      *types.OraclePricesResponse
	timestamp time.Time
}

// NewThreadSafeResponse creates a new thread-safe response.
func NewThreadSafeResponse() *ThreadSafeResponse {
	return &ThreadSafeResponse{
		resp:      nil,
		timestamp: time.Time{},
	}
}

// Update updates the response and timestamp of the thread-safe response.
func (r *ThreadSafeResponse) Update(resp *types.OraclePricesResponse) {
	r.Lock()
	defer r.Unlock()

	r.resp = resp
	r.timestamp = time.Now().UTC()
}

// Get returns the response and timestamp of the thread-safe response.
func (r *ThreadSafeResponse) Get() (*types.OraclePricesResponse, time.Time) {
	r.Lock()
	defer r.Unlock()

	return r.resp, r.timestamp
}
