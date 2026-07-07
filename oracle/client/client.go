package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	clientmetrics "noah/oracle/client/metrics"
	"noah/oracle/types"
)

var _ types.OracleClient = (*Client)(nil)

// Client defines an implementation of a gRPC oracle client. This client can
// be used in ABCI++ calls where the application wants the oracle process to be
// run out-of-process. Direct users must manage its lifecycle; CachedClient
// manages it internally when used as the cached node-side oracle client.
type Client struct {
	logger log.Logger
	mu     sync.Mutex

	// addr is the address of the remote oracle server.
	addr string
	// client is the generated oracle service client for conn.
	client types.OracleClient
	// conn is the underlying gRPC connection.
	conn *grpc.ClientConn
	// timeout bounds each RPC made through this client.
	timeout time.Duration
	// blockingDial forces Start to wait until the connection reaches Ready.
	blockingDial bool
}

// NewClient creates a new gRPC client of the oracle service with the given
// address and timeout.
func NewClient(
	logger log.Logger,
	addr string,
	timeout time.Duration,
	opts ...Option,
) (*Client, error) {
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
	}

	if timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}

	client := &Client{
		logger:  logger,
		addr:    addr,
		timeout: timeout,
	}

	// apply options
	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

// Start dials the remote oracle service and installs the generated client.
// It may block until the connection is ready when WithBlockingDial is set.
func (c *Client) Start(ctx context.Context) (err error) {
	c.logger.Info("starting oracle client", "addr", c.addr)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	conn, err := grpc.NewClient(c.addr, opts...)
	if err != nil {
		c.logger.Error("failed to dial oracle gRPC server", "err", err)
		return fmt.Errorf("failed to dial oracle gRPC server: %w", err)
	}
	started := false
	defer func() {
		if started {
			return
		}

		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close unstarted oracle gRPC connection: %w", closeErr))
		}
	}()

	if c.blockingDial {
		conn.Connect()

		for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
			if !conn.WaitForStateChange(ctx, state) {
				err := fmt.Errorf("context closed before oracle client could start: %w", ctx.Err())
				c.logger.Error("failed to dial oracle gRPC server", "err", err)
				return fmt.Errorf("failed to dial oracle gRPC server: %w", err)
			}
		}
	}

	c.mu.Lock()
	c.client = types.NewOracleClient(conn)
	c.conn = conn
	c.mu.Unlock()
	started = true

	c.logger.Info("oracle client started")

	return nil
}

// Stop closes the active gRPC connection.
func (c *Client) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("stopping oracle client")
	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.logger.Info("oracle client stopped", "err", err)

	return err
}

// Prices returns prices from the remote oracle service with the client timeout
// applied to the request context.
func (c *Client) Prices(
	ctx context.Context,
	req *types.OraclePricesRequest,
	_ ...grpc.CallOption,
) (resp *types.OraclePricesResponse, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	defer func() {
		clientmetrics.RecordOracleResponse(time.Since(start), err)
	}()

	// set deadline on the context
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if c.client == nil {
		return nil, errors.New("oracle client not started")
	}

	return c.client.Prices(ctx, req, grpc.WaitForReady(true))
}

// Version returns the version of the oracle service.
func (c *Client) Version(ctx context.Context, req *types.OracleVersionRequest, _ ...grpc.CallOption) (res *types.OracleVersionResponse, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()
	defer func() {
		clientmetrics.RecordOracleResponse(time.Since(start), err)
	}()

	// set deadline on the context
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if c.client == nil {
		return nil, errors.New("oracle client not started")
	}

	return c.client.Version(ctx, req, grpc.WaitForReady(true))
}
