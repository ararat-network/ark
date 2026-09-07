package runtime

import (
	"slices"

	sidecarmetrics "github.com/ararat-network/ark/pricefeed/sidecar/metrics"
)

// syncFeedsLocked applies the latest cached feed snapshot while the caller owns
// updateMu. Read errors preserve the current active feeds.
func (r *Runtime) syncFeedsLocked() {
	r.mut.RLock()
	client := r.client
	fromChain := r.feedsFromChain
	r.mut.RUnlock()

	feeds, err := client.Feeds()
	if err != nil {
		// Aggregation continues on the feeds already in hand, so report the
		// substitution once rather than on every tick of a chain outage.
		if !r.feedReadFailed {
			r.feedReadFailed = true
			if fromChain {
				r.logger.Warn("failed to read feeds; using last on-chain feeds", "err", err)
			} else {
				r.logger.Warn("failed to read feeds; using configured fallback feeds", "err", err)
			}
		}
		return
	}
	if r.feedReadFailed {
		r.feedReadFailed = false
		r.logger.Info("feed reads recovered")
	}

	r.mut.RLock()
	cfg := r.cfg
	oldFeeds := append([]string(nil), r.feeds...)
	mainCtx := r.mainCtx
	mainCancel := r.mainCancel
	r.mut.RUnlock()

	if slices.Equal(oldFeeds, feeds) {
		r.mut.Lock()
		r.feedsFromChain = true
		r.mut.Unlock()
		return
	}

	oldPairs := cfg.Resolver.MarketPairs(oldFeeds)
	newPairs := cfg.Resolver.MarketPairs(feeds)

	updates := make([]providerMarketUpdate, 0, len(r.providers))
	for name, managed := range r.providers {
		providerCfg, ok := cfg.Providers[name]
		if !ok {
			continue
		}
		oldMarkets := providerCfg.Markets.FilterPairs(oldPairs)
		newMarkets := providerCfg.Markets.FilterPairs(newPairs)
		if oldMarkets.Equal(newMarkets) {
			continue
		}
		updates = append(updates, providerMarketUpdate{
			managed: managed,
			markets: newMarkets,
		})
	}
	for _, update := range updates {
		update.managed.stop()
	}
	for _, update := range updates {
		update.managed.provider.UpdateMarkets(update.markets)
	}

	r.mut.Lock()
	r.feeds = append([]string(nil), feeds...)
	sidecarmetrics.PublishAggregationSnapshot(sidecarmetrics.AggregationSnapshot{})
	r.feedsFromChain = true
	r.mut.Unlock()

	if mainCtx == nil || mainCtx.Err() != nil {
		return
	}
	for _, update := range updates {
		update.managed.start(mainCtx, mainCancel, r.logger)
	}
}
