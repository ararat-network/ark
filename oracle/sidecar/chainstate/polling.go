package chainstate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"google.golang.org/grpc"

	chainstatemetrics "ark/oracle/sidecar/chainstate/metrics"
	oracletypes "ark/x/oracle/types"
)

// runOnce opens one query connection for the current address and polls it until
// cancellation, reconnect, or an unrecoverable connection-level failure.
func (c *Client) runOnce(ctx context.Context) (err error) {
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
	return c.poll(ctx, cfg.Address, cfg.Interval, query)
}

// poll refreshes vote targets immediately and then at the configured interval.
// Config changes are observed on ticks. Address updates end this connection so
// Run can reconnect with fresh config.
func (c *Client) poll(
	ctx context.Context,
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
		case <-ticker.C:
			nextCfg := c.getConfig()
			if nextCfg.Address != address {
				c.logger.Info("reconnecting chain state vote-target client after address update")
				return nil
			}
			if nextCfg.Interval != interval {
				c.logger.Debug("updated chain state vote-target poll interval")
				ticker.Reset(nextCfg.Interval)
				interval = nextCfg.Interval
			}
			c.refresh(ctx, query)
		}
	}
}

// refresh commits successful snapshots. Query failures log a warning and
// preserve the previous snapshot so the runtime can keep using last-known vote
// targets with operator-visible refresh errors.
func (c *Client) refresh(ctx context.Context, query oracletypes.QueryClient) {
	targets, err := c.queryVoteTargets(ctx, query)
	if err != nil {
		chainstatemetrics.RecordRefresh(ctx, "error")
		c.logger.Warn("failed to refresh chain state vote targets", "error", err)
		return
	}

	c.mut.Lock()
	c.targets = targets
	c.hasSnapshot = true
	c.mut.Unlock()

	chainstatemetrics.RecordRefresh(ctx, "success")
}

// queryVoteTargets performs one oracle query and combines active and pending
// targets before the caller stores them as the cached snapshot.
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

	denoms := resp.VoteTargets
	if resp.Pending != nil {
		denoms = append(denoms, resp.Pending.Denoms...)
	}
	slices.Sort(denoms)
	return slices.Compact(denoms), nil
}

// waitForRetry sleeps until the next retry or cancellation.
func waitForRetry(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
