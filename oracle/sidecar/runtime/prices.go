package runtime

import (
	"context"
	"time"

	oraclemetrics "noah/oracle/sidecar/metrics"
	"noah/oracle/sidecar/providers/base"
	"noah/oracle/sidecar/types"
)

// fetchAllPrices syncs the effective vote-target snapshot, copies fresh provider prices,
// and updates aggregate price state for one runtime tick.
func (r *Runtime) fetchAllPrices(ctx context.Context) {
	r.logger.Debug("starting price fetch loop")

	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	r.refreshVoteTargetsLocked()
	r.resolver.Reset()

	r.mut.RLock()
	providers := make([]*base.Provider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	maxPriceAge := r.cfg.MaxPriceAge
	denoms := append([]string(nil), r.denoms...)
	r.mut.RUnlock()

	for _, provider := range providers {
		r.fetchPrices(provider, maxPriceAge)
	}
	r.logger.Debug("oracle fetched prices from providers")

	r.resolver.ResolvePrices(denoms)

	prices := types.PricesByDenom(r.resolver.GetPrices(), denoms)
	r.recordMissingPrices(ctx, denoms, prices)
	r.commitPriceSnapshot(prices, time.Now().UTC())
	oraclemetrics.RecordOracleTick(ctx)
}

// fetchPrices copies one provider's fresh cached prices into the resolver.
func (r *Runtime) fetchPrices(provider *base.Provider, maxPriceAge time.Duration) {
	if !provider.IsRunning() {
		r.logger.Debug(
			"provider is not running",
			"provider", provider.Name(),
		)

		return
	}

	r.logger.Debug(
		"retrieving prices",
		"provider", provider.Name(),
		"data handler type", provider.Type(),
	)

	prices := provider.GetPrices()
	if prices == nil {
		r.logger.Debug(
			"provider returned nil prices",
			"provider", provider.Name(),
			"data handler type", provider.Type(),
		)

		return
	}

	timeFilteredPrices := make(types.Prices)
	for pair, result := range prices {
		diff := time.Now().UTC().Sub(result.Timestamp)
		if diff > maxPriceAge {
			r.logger.Debug(
				"skipping price",
				"provider", provider.Name(),
				"data handler type", provider.Type(),
				"pair", pair,
				"diff", diff,
			)

			continue
		}

		r.logger.Debug(
			"adding price",
			"provider", provider.Name(),
			"data handler type", provider.Type(),
			"pair", pair,
			"price", result.Price,
			"diff", diff,
		)
		timeFilteredPrices[pair] = result.Price
	}

	r.logger.Debug("provider returned prices",
		"provider", provider.Name(),
		"data handler type", provider.Type(),
		"prices", len(prices),
	)
	r.resolver.SetProviderPrices(provider.Name(), timeFilteredPrices)
}

// refreshVoteTargetsLocked applies the latest cached vote-target snapshot while
// the caller owns updateMu. Before the first successful snapshot it falls back
// to configured denoms; after a successful snapshot it preserves the last-known
// vote targets on read errors.
func (r *Runtime) refreshVoteTargetsLocked() {
	r.mut.RLock()
	client := r.client
	fallbackDenoms := append([]string(nil), r.cfg.FallbackDenoms...)
	hasVoteTargets := r.denomsFromVoteTargets
	r.mut.RUnlock()

	denoms, err := client.VoteTargets()
	if err != nil {
		if hasVoteTargets {
			r.logger.Warn("failed to refresh vote targets; using last known denoms", "err", err)
			return
		}

		r.setDenomsLocked(fallbackDenoms, false)
		r.logger.Warn("failed to refresh vote targets; using fallback config denoms", "err", err)
		return
	}

	r.setDenomsLocked(denoms, true)
}

// setDenomsLocked stores active denoms while the caller owns updateMu. It
// retargets provider markets derived from the old and new denom snapshots, then
// restarts affected providers after mutable runtime state has been committed.
func (r *Runtime) setDenomsLocked(denoms []string, fromVoteTargets bool) {
	r.mut.RLock()
	cfg := r.cfg.Clone()
	oldDenoms := append([]string(nil), r.denoms...)
	oldProviders := make(map[string]*base.Provider, len(r.providers))
	for name, provider := range r.providers {
		oldProviders[name] = provider
	}
	running := r.running.Load()
	mainCtx := r.mainCtx
	r.mut.RUnlock()

	oldPairs := cfg.Resolver.MarketPairs(oldDenoms)
	newPairs := cfg.Resolver.MarketPairs(denoms)

	updates := make([]providerUpdate, 0, len(oldProviders))
	for name, provider := range oldProviders {
		providerCfg, ok := cfg.Providers[name]
		if !ok {
			continue
		}
		oldMarkets := providerCfg.Markets.FilterPairs(oldPairs)
		newMarkets := providerCfg.Markets.FilterPairs(newPairs)
		if oldMarkets.Equal(newMarkets) {
			continue
		}
		updates = append(updates, providerUpdate{
			provider: provider,
			markets:  newMarkets,
		})
	}

	r.mut.Lock()
	for _, update := range updates {
		update.provider.Update(update.markets)
	}
	r.denoms = append([]string(nil), denoms...)
	r.denomsFromVoteTargets = fromVoteTargets
	r.mut.Unlock()

	if !running || mainCtx == nil {
		return
	}
	for _, update := range updates {
		if err := update.provider.Start(mainCtx); err != nil {
			r.logProviderStartError(mainCtx, update.provider.Name(), err)
		}
	}
}

// recordMissingPrices records expected denoms missing from the latest aggregate prices.
func (r *Runtime) recordMissingPrices(ctx context.Context, denoms []string, prices types.DenomPrices) {
	if len(denoms) == 0 {
		return
	}

	missing := make([]string, 0, len(denoms))
	for _, denom := range denoms {
		if _, ok := prices[denom]; !ok {
			missing = append(missing, denom)
		}
	}

	if len(missing) > 0 {
		r.logger.Warn(
			"oracle missing prices for active vote targets",
			"denoms", missing,
			"count", len(missing),
		)
	}

	oraclemetrics.RecordMissingPrices(ctx, missing)
}

// commitPriceSnapshot stores a runtime-owned price snapshot from one aggregation
// tick. The caller must not mutate prices after commit.
func (r *Runtime) commitPriceSnapshot(prices types.DenomPrices, timestamp time.Time) {
	r.mut.Lock()
	defer r.mut.Unlock()

	r.priceSnapshot = types.PriceSnapshot{
		Prices:    prices,
		Timestamp: timestamp,
	}
}
