package chainstate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"
)

// Client polls the chain's oracle query service for vote-target denoms and
// serves the latest valid snapshot to the sidecar runtime.
type Client struct {
	logger      log.Logger
	dialOptions []grpc.DialOption

	mut sync.RWMutex

	// Config is mutable through Update and read by the polling loop.
	cfg Config

	// Cached chain state. targets is the last valid snapshot; lastErr is only
	// surfaced while no snapshot exists.
	targets []string
	lastErr error

	// Poll-loop lifecycle. These are set by Start, cleared by run, and read by
	// Stop/Update under mut.
	isRunning atomic.Bool
	cancleFn  context.CancelFunc
	doneCh    chan struct{}
	updateCh  chan struct{}
}

// NewClient validates cfg, applies opts, and returns a stopped chainstate
// client ready to be started by the sidecar owner.
func NewClient(cfg Config, opts ...Option) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	c := &Client{
		logger: log.NewNopLogger(),
		cfg:    cfg,
		dialOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}
	for _, opt := range opts {
		opt(c)
	}

	if c.logger == nil {
		return nil, errors.New("logger is nil")
	}
	c.logger = c.logger.With("chain_state_client", "vote_targets")

	return c, nil
}

// Start launches the background poll loop. It returns after the loop has been
// initialised; callers should use VoteTargets to read the cached snapshot.
func (c *Client) Start(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	updateCh := make(chan struct{}, 1)
	doneCh := make(chan struct{})

	c.mut.Lock()
	if c.isRunning.Load() {
		c.mut.Unlock()
		cancel()
		return errors.New("vote targets client already running")
	}
	c.isRunning.Store(true)
	c.cancleFn = cancel
	c.doneCh = doneCh
	c.updateCh = updateCh
	c.mut.Unlock()

	cfg := c.getConfig()
	c.logger.Info(
		"starting chain state vote-target client",
		"address", cfg.Address,
		"interval", cfg.Interval,
		"timeout", cfg.Timeout,
	)

	go c.run(runCtx, cancel, updateCh, doneCh)

	return nil
}

// Stop cancels the poll loop and waits until it has released its lifecycle
// state. Calling Stop on an already stopped client is a no-op.
func (c *Client) Stop() {
	c.mut.RLock()
	cancel := c.cancleFn
	doneCh := c.doneCh
	c.mut.RUnlock()

	if doneCh == nil {
		c.logger.Debug("chain state vote-target client is not running")
		return
	}

	c.logger.Info("stopping chain state vote-target client")
	if cancel != nil {
		cancel()
	}
	<-doneCh
	c.logger.Info("chain state vote-target client stopped")
}

// Update replaces the client config and wakes the polling loop. The caller must
// validate cfg before calling Update. Address changes reconnect the query
// client; timeout and interval changes take effect on the next loop wake-up.
func (c *Client) Update(cfg Config) {
	c.mut.Lock()
	if c.cfg.Equal(cfg) {
		c.mut.Unlock()
		return
	}

	c.cfg = cfg

	if c.updateCh != nil {
		select {
		case c.updateCh <- struct{}{}:
		default:
		}
	}
	c.mut.Unlock()

	c.logger.Info("updated chain state vote-target client config")
}

// VoteTargets returns a copy of the latest valid vote-target snapshot. It only
// returns the latest refresh error while no successful snapshot has been cached.
func (c *Client) VoteTargets() ([]string, error) {
	c.mut.RLock()
	defer c.mut.RUnlock()

	if len(c.targets) == 0 {
		if c.lastErr != nil {
			return nil, fmt.Errorf("no vote targets fetched yet: %w", c.lastErr)
		}
		return nil, errors.New("no vote targets fetched yet")
	}

	return append([]string(nil), c.targets...), nil
}

// getConfig returns a copy of the current polling config under the client lock.
func (c *Client) getConfig() Config {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return c.cfg
}
