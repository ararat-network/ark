package runtime

import "slices"

// syncVoteTargetsLocked applies the latest cached vote-target snapshot while
// the caller owns updateMu. Read errors preserve the current active denoms.
func (r *Runtime) syncVoteTargetsLocked() {
	r.mut.RLock()
	client := r.client
	r.mut.RUnlock()

	denoms, err := client.VoteTargets()
	if err != nil {
		r.logger.Warn("failed to read vote targets; using current denoms", "err", err)
		return
	}

	r.mut.RLock()
	cfg := r.cfg
	oldDenoms := append([]string(nil), r.denoms...)
	mainCtx := r.mainCtx
	mainCancel := r.mainCancel
	r.mut.RUnlock()

	if slices.Equal(oldDenoms, denoms) {
		r.mut.Lock()
		r.denomsFromVoteTargets = true
		r.mut.Unlock()
		return
	}

	oldPairs := cfg.Resolver.MarketPairs(oldDenoms)
	newPairs := cfg.Resolver.MarketPairs(denoms)

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
	r.denoms = append([]string(nil), denoms...)
	r.denomsFromVoteTargets = true
	r.mut.Unlock()

	if mainCtx == nil || mainCtx.Err() != nil {
		return
	}
	for _, update := range updates {
		update.managed.start(mainCtx, mainCancel, r.logger)
	}
}
