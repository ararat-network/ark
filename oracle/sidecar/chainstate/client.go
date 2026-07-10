package chainstate

import (
	"context"
	"errors"
	"sync"

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

	// Cached chain state. targets is the last valid snapshot.
	targets []string
}

// NewClient validates cfg, applies opts, and returns a chainstate client ready
// to be run by the sidecar owner.
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

// Run blocks on the poll loop and cleans up lifecycle state before returning.
// Callers should run it in the owning lifecycle and use VoteTargets to read the
// cached snapshot.
func (c *Client) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	c.logger.Info("starting chain state client")

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := c.runOnce(ctx); err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return ctx.Err()
			}

			c.logger.Error("chain state client poll failed", "error", err)

			cfg := c.getConfig()
			if err := waitForRetry(ctx, cfg.Interval); err != nil {
				return err
			}
		}
	}
}

// Update replaces the client config. The caller must validate cfg before
// calling Update. Run observes address, timeout, and interval changes on the
// next poll or retry cycle.
func (c *Client) Update(cfg Config) {
	c.mut.Lock()
	if c.cfg.Equal(cfg) {
		c.mut.Unlock()
		return
	}
	c.cfg = cfg
	c.mut.Unlock()

	c.logger.Info("updated chain state vote-target client config")
}

// VoteTargets returns a copy of the latest valid vote-target snapshot. It
// returns an error until the first successful non-empty snapshot is cached;
// later refresh failures preserve the last successful snapshot.
func (c *Client) VoteTargets() ([]string, error) {
	c.mut.RLock()
	defer c.mut.RUnlock()

	if len(c.targets) == 0 {
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
