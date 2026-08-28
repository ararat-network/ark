package chainstate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"google.golang.org/grpc"

	chainstatemetrics "github.com/ararat-network/ark/pricefeed/sidecar/chainstate/metrics"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
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

// poll refreshes feeds immediately and then at the configured interval.
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
				c.logger.Info("reconnecting chain state feed client after address update")
				return nil
			}
			if nextCfg.Interval != interval {
				c.logger.Debug("updated chain state feed poll interval")
				ticker.Reset(nextCfg.Interval)
				interval = nextCfg.Interval
			}
			c.refresh(ctx, query)
		}
	}
}

// refresh commits successful snapshots. Query failures log a warning and
// preserve the previous snapshot so the runtime can keep using the last-known
// feed set with operator-visible refresh errors.
func (c *Client) refresh(ctx context.Context, query oracletypes.QueryClient) {
	feeds, err := c.queryFeeds(ctx, query)
	if err != nil {
		chainstatemetrics.RecordRefresh(ctx, "error")
		c.logger.Warn("failed to refresh chain state feeds", "error", err)
		return
	}

	c.mut.Lock()
	c.feeds = feeds
	c.hasSnapshot = true
	c.mut.Unlock()

	chainstatemetrics.RecordRefresh(ctx, "success")
}

// queryFeeds performs one oracle query and unions the active feeds with every
// scheduled addition before the caller stores them as the cached snapshot.
// Warming an addition through its activation delay is what lets a capable fleet
// price a new feed from its first active block; scheduled removals are excluded
// because the feed is still active and already in the union.
func (c *Client) queryFeeds(ctx context.Context, query oracletypes.QueryClient) ([]string, error) {
	cfg := c.getConfig()
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	resp, err := query.Feeds(ctx, &oracletypes.QueryFeedsRequest{})
	if err != nil {
		return nil, fmt.Errorf("query oracle feeds: %w", err)
	}
	if resp == nil {
		return nil, errors.New("oracle feeds response is nil")
	}
	if len(resp.Feeds.Denoms) > oracletypes.MaxFeeds {
		return nil, fmt.Errorf(
			"active feed count %d exceeds maximum %d",
			len(resp.Feeds.Denoms),
			oracletypes.MaxFeeds,
		)
	}
	if len(resp.Feeds.Transitions) > oracletypes.MaxFeeds {
		return nil, fmt.Errorf(
			"scheduled feed transition count %d exceeds maximum %d",
			len(resp.Feeds.Transitions),
			oracletypes.MaxFeeds,
		)
	}

	feeds := slices.Clone(resp.Feeds.Denoms)
	for _, transition := range resp.Feeds.Transitions {
		if transition.Direction == oracletypes.FeedDirection_FEED_DIRECTION_ADD {
			feeds = append(feeds, transition.Denom)
		}
	}
	slices.Sort(feeds)
	return slices.Compact(feeds), nil
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
