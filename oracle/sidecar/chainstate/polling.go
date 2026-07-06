package chainstate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chainstatemetrics "noah/oracle/sidecar/chainstate/metrics"
	oracletypes "noah/x/oracle/types"
)

// run owns the long-lived polling lifecycle and clears Start state before
// returning. Connection-level failures are recorded, logged, and retried.
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

// runOnce opens one query connection for the current address and polls it until
// cancellation, reconnect, or an unrecoverable connection-level failure.
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

// poll refreshes vote targets immediately and then at the configured interval.
// Address updates end this connection so run can reconnect with fresh config.
func (c *Client) poll(
	ctx context.Context,
	updateCh <-chan struct{},
	address string,
	interval time.Duration,
	query oracletypes.QueryClient,
) error {
	c.refresh(ctx, query)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-updateCh:
			nextCfg := c.getConfig()
			if nextCfg.Address != address {
				c.logger.Info("reconnecting chain state vote-target client after address update")
				return nil
			}
			if nextCfg.Interval != interval {
				c.logger.Debug(
					"updated chain state vote-target poll interval",
					"old_interval", interval,
					"new_interval", nextCfg.Interval,
				)
				ticker.Reset(nextCfg.Interval)
				interval = nextCfg.Interval
			}
		case <-ticker.C:
			c.refresh(ctx, query)
		}
	}
}

// refresh commits only valid snapshots. Failures update lastErr, log a warning,
// and preserve the previous snapshot so the runtime can keep using last-known
// vote targets with operator-visible refresh errors.
func (c *Client) refresh(ctx context.Context, query oracletypes.QueryClient) {
	targets, err := c.queryVoteTargets(ctx, query)
	if err != nil {
		c.mut.Lock()
		c.lastErr = err
		c.mut.Unlock()

		chainstatemetrics.RecordRefresh(ctx, "error")
		c.logger.Warn("failed to refresh chain state vote targets", "error", err)
		return
	}

	c.mut.Lock()
	c.targets = targets
	c.lastErr = nil
	c.mut.Unlock()

	chainstatemetrics.RecordRefresh(ctx, "success")
}

// queryVoteTargets performs one oracle query and validates the response before
// the caller stores it as the cached snapshot.
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

// waitForRetry sleeps until the next retry, a config update, or cancellation.
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
