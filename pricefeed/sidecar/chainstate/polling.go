package chainstate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ararat-network/ark/pkg/grpcconn"
	chainstatemetrics "github.com/ararat-network/ark/pricefeed/sidecar/chainstate/metrics"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// endpoint is one dialled chain node. runOnce builds one per configured
// address.
type endpoint struct {
	address string
	query   oracletypes.QueryClient
}

// runOnce opens one query connection per configured address and polls them
// until cancellation, reconnect, or an unrecoverable connection-level failure.
func (c *Client) runOnce(ctx context.Context) (err error) {
	cfg, material := c.snapshot()
	chainstatemetrics.SetEndpoints(ctx, cfg.Addresses)
	stopCertificates := material.Start(ctx, c.logger)
	defer stopCertificates()
	dial := grpcconn.DialOptions(material.Config, c.dialOptions...)

	conns, err := grpcconn.Open(cfg.Addresses, dial...)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := grpcconn.Close(conns)
		if closeErr == nil {
			return
		}
		// A close failure on a clean reconnect stays a log line: returning it
		// would make Run treat the reconnect as a poll failure and sleep out
		// an interval before redialling.
		if err != nil {
			err = errors.Join(err, closeErr)
			return
		}
		c.logger.Error("failed to close oracle query connections", "error", closeErr)
	}()
	endpoints := make([]endpoint, 0, len(conns))
	for i, conn := range conns {
		endpoints = append(endpoints, endpoint{
			address: cfg.Addresses[i],
			query:   oracletypes.NewQueryClient(conn),
		})
	}

	// Reconcile against the list actually opened: another update can arrive
	// between poll requesting a reconnect and this run taking its snapshot.
	if !slices.Equal(c.activeAddresses, cfg.Addresses) {
		c.active = 0
		c.activeAddresses = slices.Clone(cfg.Addresses)
	}

	return c.poll(ctx, cfg, endpoints)
}

// poll refreshes feeds immediately and then at the configured interval.
// Config changes are observed on ticks. An address or TLS update ends these
// connections so Run can reconnect with fresh config.
func (c *Client) poll(ctx context.Context, cfg Config, endpoints []endpoint) error {
	c.refresh(ctx, endpoints)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			nextCfg := c.getConfig()
			if !slices.Equal(nextCfg.Addresses, cfg.Addresses) {
				c.logger.Info("reconnecting chain state feed client after address update")
				return nil
			}
			if nextCfg.TLS != cfg.TLS {
				c.logger.Info("reconnecting chain state feed client after tls update")
				return nil
			}
			if nextCfg.Interval != cfg.Interval {
				c.logger.Debug("updated chain state feed poll interval")
				ticker.Reset(nextCfg.Interval)
				cfg.Interval = nextCfg.Interval
			}
			c.refresh(ctx, endpoints)
		}
	}
}

// refresh tries the active endpoint, then the rest in order, and commits the
// first snapshot that answers. The endpoint that answered becomes active and
// stays so until it fails: there is no fail-back, so a flapping preferred node
// cannot bounce the client back and forth. A sweep in which every endpoint
// fails logs a warning and preserves the previous snapshot, so the runtime
// keeps using the last-known feed set with operator-visible refresh errors.
// Cancellation ends the sweep.
func (c *Client) refresh(ctx context.Context, endpoints []endpoint) {
	var errs []error
	for offset := range endpoints {
		if ctx.Err() != nil {
			return
		}
		i := (c.active + offset) % len(endpoints)
		feeds, err := c.queryFeeds(ctx, endpoints[i].query)
		chainstatemetrics.RecordRefresh(ctx, endpoints[i].address, refreshStatus(err))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", endpoints[i].address, err))
			continue
		}

		if i != c.active {
			c.logger.Warn(
				"failing over to chain node",
				"from", endpoints[c.active].address,
				"to", endpoints[i].address,
			)
			c.active = i
		}

		c.mut.Lock()
		c.feeds = feeds
		c.hasSnapshot = true
		c.mut.Unlock()

		return
	}

	if ctx.Err() != nil {
		return
	}
	c.logger.Warn(
		"failed to refresh chain state feeds from every chain node",
		"error", errors.Join(errs...),
	)
}

// refreshStatus is the metric label for one attempt's outcome.
func refreshStatus(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
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
