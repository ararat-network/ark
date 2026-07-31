package runtime

import "slices"

// syncFeedsLocked applies the latest cached feed snapshot while the caller owns
// updateMu. Read errors preserve the current active feeds.
func (r *Runtime) syncFeedsLocked() {
	r.mut.RLock()
	client := r.client
	r.mut.RUnlock()

	feeds, err := client.Feeds()
	if err != nil {
		r.logger.Warn("failed to read feeds; using current feeds", "err", err)
		return
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
	r.feedsFromChain = true
	r.mut.Unlock()

	if mainCtx == nil || mainCtx.Err() != nil {
		return
	}
	for _, update := range updates {
		update.managed.start(mainCtx, mainCancel, r.logger)
	}
}
