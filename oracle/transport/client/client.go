package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	clientmetrics "noah/oracle/transport/client/metrics"
	"noah/oracle/transport/types"
)

var _ types.OracleClient = (*Client)(nil)

// Client defines an implementation of a gRPC oracle client. This client can
// be used in ABCI++ calls where the application wants the oracle process to be
// run out-of-process. Direct users must manage its lifecycle; PriceDaemon
// manages it internally when used as the cached node-side oracle client.
type Client struct {
	logger log.Logger
	mu     sync.Mutex

	// address of remote oracle server
	addr string
	// underlying oracle client
	client types.OracleClient
	// underlying grpc connection
	conn *grpc.ClientConn
	// timeout for the client, Price requests will block for this duration.
	timeout time.Duration
	// blockingDial is a parameter which determines whether the client should block on dialing the server
	blockingDial bool
}

// NewClient creates a new grpc client of the oracle service with the given
// address and timeout.
func NewClient(
	logger log.Logger,
	addr string,
	timeout time.Duration,
	opts ...Option,
) (*Client, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be positive")
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

// Start starts the GRPC client. This method dials the remote oracle-service
// and errors if the connection fails. This method may block (depending on the blockingDial option).
func (c *Client) Start(ctx context.Context) error {
	c.logger.Info("starting oracle client", "addr", c.addr)

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	// dial the client, but defer to context closure, if necessary
	var (
		conn *grpc.ClientConn
		err  error
		done = make(chan struct{})
	)
	go func() {
		defer close(done)
		conn, err = grpc.NewClient(c.addr, opts...)

		// attempt to connect + wait for change in connection state
		if c.blockingDial {
			// connect
			conn.Connect()

			if err == nil {
				conn.WaitForStateChange(ctx, connectivity.Ready)
			}
		}
	}()

	// wait for either the context to close or the dial to complete
	select {
	case <-ctx.Done():
		err = fmt.Errorf("context closed before oracle client could start: %w", ctx.Err())
	case <-done:
	}
	if err != nil {
		c.logger.Error("failed to dial oracle gRPC server", "err", err)
		return fmt.Errorf("failed to dial oracle gRPC server: %w", err)
	}

	c.mu.Lock()
	c.client = types.NewOracleClient(conn)
	c.conn = conn
	c.mu.Unlock()

	c.logger.Info("oracle client started")

	return nil
}

// Stop stops the GRPC client. This method closes the connection to the remote.
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

// Prices returns the prices from the remote oracle service. This method blocks for the timeout duration configured on the client,
// otherwise it returns the response from the remote oracle.
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
		return nil, fmt.Errorf("oracle client not started")
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
		return nil, fmt.Errorf("oracle client not started")
	}

	return c.client.Version(ctx, req, grpc.WaitForReady(true))
}
