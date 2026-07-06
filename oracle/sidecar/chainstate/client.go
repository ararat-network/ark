package chainstate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "noah/x/oracle/types"
)

type Client struct {
	logger      log.Logger
	cfg         Config
	dialOptions []grpc.DialOption

	mut     sync.RWMutex
	targets []string
	lastErr error

	isRunning atomic.Bool
	cancleFn  context.CancelFunc
	doneCh    chan struct{}
	updateCh  chan struct{}
}

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

	go c.run(runCtx, cancel, updateCh, doneCh)

	return nil
}

func (c *Client) Stop() {
	c.mut.RLock()
	cancel := c.cancleFn
	doneCh := c.doneCh
	c.mut.RUnlock()

	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (c *Client) run(ctx context.Context, cancel context.CancelFunc, updateCh, doneCh chan struct{}) {
	defer func() {
		cancel()

		c.mut.Lock()
		c.isRunning.Store(false)
		c.cancleFn = nil
		close(updateCh)
		c.updateCh = nil
		close(doneCh)
		c.doneCh = nil
		c.mut.Unlock()
	}()

	for {
		if err := c.runOnce(ctx, updateCh); err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return
			}

			c.mut.Lock()
			c.lastErr = err
			c.mut.Unlock()
			c.logger.Error("chain state client poll failed", "error", err)

			cfg := c.getConfig()
			if err := waitForRetry(ctx, updateCh, cfg.Interval); err != nil {
				return
			}
		}
	}
}

func (c *Client) runOnce(ctx context.Context, updateCh <-chan struct{}) (err error) {
	defer func() {
		if recErr := recover(); recErr != nil {
			err = fmt.Errorf("chain state client panicked: %v", recErr)
		}
	}()

	cfg := c.getConfig()
	conn, err := grpc.NewClient(cfg.Address, c.dialOptions...)
	if err != nil {
		return fmt.Errorf("create oracle query connection: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			if err != nil {
				err = errors.Join(err, fmt.Errorf("close oracle query connection: %w", closeErr))
				return
			}
			c.logger.Error("failed to close oracle query connection", "error", closeErr)
		}
	}()

	query := oracletypes.NewQueryClient(conn)
	return c.poll(ctx, updateCh, cfg.Address, cfg.Interval, query)
}

// Update replaces the client config and wakes the polling loop. The caller must
// validate cfg before calling Update.
func (c *Client) Update(cfg Config) {
	c.mut.Lock()
	defer c.mut.Unlock()

	if c.cfg.Equal(cfg) {
		return
	}
	c.cfg = cfg

	if c.updateCh != nil {
		select {
		case c.updateCh <- struct{}{}:
		default:
		}
	}
}

func (c *Client) poll(
	ctx context.Context,
	updateCh <-chan struct{},
	address string,
	interval time.Duration,
	query oracletypes.QueryClient,
) error {
	c.refresh(ctx, address, query)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-updateCh:
			nextCfg := c.getConfig()
			if nextCfg.Address != address {
				return nil
			}
			if nextCfg.Interval != interval {
				ticker.Reset(nextCfg.Interval)
				interval = nextCfg.Interval
			}
		case <-ticker.C:
			c.refresh(ctx, address, query)
		}
	}
}

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

func (c *Client) refresh(ctx context.Context, address string, query oracletypes.QueryClient) {
	targets, err := c.queryVoteTargets(ctx, query)

	c.mut.Lock()
	defer c.mut.Unlock()

	if c.cfg.Address != address {
		return
	}
	if err != nil {
		c.lastErr = err
		return
	}

	c.targets = targets
	c.lastErr = nil
}

func (c *Client) queryVoteTargets(ctx context.Context, query oracletypes.QueryClient) ([]string, error) {
	cfg := c.getConfig()
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	resp, err := query.VoteTargets(ctx, &oracletypes.QueryVoteTargetsRequest{})
	if err != nil {
		return nil, fmt.Errorf("query oracle vote targets: %w", err)
	}
	if resp == nil {
		return nil, errors.New("oracle vote targets response is nil")
	}

	targets := append([]string(nil), resp.VoteTargets...)
	if len(targets) == 0 {
		return nil, errors.New("oracle vote targets response is empty")
	}

	seen := make(map[string]struct{}, len(targets))
	for _, denom := range targets {
		if err := sdk.ValidateDenom(denom); err != nil {
			return nil, fmt.Errorf("invalid vote target denom %q: %w", denom, err)
		}
		if _, ok := seen[denom]; ok {
			return nil, fmt.Errorf("duplicate vote target denom %q", denom)
		}
		seen[denom] = struct{}{}
	}
	return targets, nil
}

func (c *Client) getConfig() Config {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return c.cfg
}

func waitForRetry(ctx context.Context, updateCh <-chan struct{}, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-updateCh:
		return nil
	case <-timer.C:
		return nil
	}
}
