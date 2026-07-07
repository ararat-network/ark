package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	"noah/oracle/types"
)

// CachedClient polls the sidecar over gRPC and serves a fresh-enough cached
// price response to node-side callers.
type CachedClient struct {
	logger log.Logger

	// config controls polling cadence, freshness, and the underlying gRPC client.
	config Config
	// client is the underlying oracle client used to fetch prices.
	client *Client
	// resp is the latest price response fetched by the polling loop.
	resp ThreadSafeResponse

	// lifecycleMu guards cancel and doneCh for the active polling run.
	lifecycleMu sync.Mutex
	// cancel and doneCh describe the active polling run. Stop cancels the run
	// context and waits for doneCh to close after Start has released resources.
	cancel context.CancelFunc
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
	}, nil
}

// Start connects the underlying gRPC client and runs the price polling loop.
// This method blocks until the cached client is stopped or the context is cancelled.
func (d *CachedClient) Start(ctx context.Context) (err error) {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})

	d.lifecycleMu.Lock()
	if d.doneCh != nil {
		d.lifecycleMu.Unlock()
		cancel()
		return errors.New("cached client already running")
	}
	d.cancel = cancel
	d.doneCh = doneCh
	d.lifecycleMu.Unlock()

	defer func() {
		cancel()
		err = errors.Join(err, d.client.Stop())

		d.lifecycleMu.Lock()
		d.cancel = nil
		d.doneCh = nil
		close(doneCh)
		d.lifecycleMu.Unlock()
	}()

	if err := d.client.Start(runCtx); err != nil {
		if runCtx.Err() != nil && ctx.Err() == nil {
			return nil
		}
		return err
	}

	ticker := time.NewTicker(d.config.Interval)
	defer ticker.Stop()

	d.logger.Info("starting price daemon")

	for {
		select {
		case <-runCtx.Done():
			if ctx.Err() == nil {
				d.logger.Info("price daemon stopped")
				return nil
			}
			d.logger.Info("stopping price daemon from context")
			return ctx.Err()
		case <-ticker.C:
			d.fetchPrices(runCtx)
		}
	}
}

// fetchPrices fetches the latest prices from the oracle client.
func (d *CachedClient) fetchPrices(ctx context.Context) {
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

// Prices returns the latest cached price response. If the latest response is too
// stale, an error is returned.
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

// Stop stops the polling loop and waits for the underlying gRPC client to close.
func (d *CachedClient) Stop() error {
	d.lifecycleMu.Lock()
	cancel := d.cancel
	doneCh := d.doneCh
	d.lifecycleMu.Unlock()

	if doneCh == nil {
		return nil
	}

	if cancel != nil {
		cancel()
	}
	<-doneCh

	return nil
}

// ThreadSafeResponse is a thread-safe wrapper around an OraclePricesResponse.
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
