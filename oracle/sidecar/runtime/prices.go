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
func (o *Runtime) fetchAllPrices(ctx context.Context) {
	o.logger.Debug("starting price fetch loop")
	defer func() {
		if r := recover(); r != nil {
			o.logger.Error("fetchAllPrices tick panicked", "error", r)
		}
	}()

	o.syncVoteTargets()
	o.resolver.Reset()

	o.mut.RLock()
	providers := make([]*base.Provider, 0, len(o.providers))
	for _, provider := range o.providers {
		providers = append(providers, provider)
	}

	maxPriceAge := o.cfg.MaxPriceAge
	denoms := append([]string(nil), o.denoms...)
	o.mut.RUnlock()

	for _, provider := range providers {
		o.fetchPrices(provider, maxPriceAge)
	}

	o.logger.Debug("oracle fetched prices from providers")

	o.resolver.ResolvePrices(denoms)
	o.recordMissingPrices(ctx, denoms)
	o.setLastSyncTime(time.Now().UTC())
	oraclemetrics.RecordOracleTick(ctx)
}

// fetchPrices copies one provider's fresh cached prices into the resolver.
func (o *Runtime) fetchPrices(provider *base.Provider, maxPriceAge time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			o.logger.Error(
				"provider panicked",
				"provider_name", provider.Name(),
				"error", r,
			)
		}
	}()

	if !provider.IsRunning() {
		o.logger.Debug(
			"provider is not running",
			"provider", provider.Name(),
		)

		return
	}

	o.logger.Debug(
		"retrieving prices",
		"provider", provider.Name(),
		"data handler type", provider.Type(),
	)

	prices := provider.GetPrices()
	if prices == nil {
		o.logger.Debug(
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
			o.logger.Debug(
				"skipping price",
				"provider", provider.Name(),
				"data handler type", provider.Type(),
				"pair", pair,
				"diff", diff,
			)

			continue
		}

		o.logger.Debug(
			"adding price",
			"provider", provider.Name(),
			"data handler type", provider.Type(),
			"pair", pair,
			"price", result.Price,
			"diff", diff,
		)
		timeFilteredPrices[pair] = result.Price
	}

	o.logger.Debug("provider returned prices",
		"provider", provider.Name(),
		"data handler type", provider.Type(),
		"prices", len(prices),
	)
	o.resolver.SetProviderPrices(provider.Name(), timeFilteredPrices)
}

// syncVoteTargets applies the latest cached vote-target snapshot. Before the
// first successful snapshot it falls back to configured denoms; after a
// successful snapshot it preserves the last-known vote targets on read errors.
func (o *Runtime) syncVoteTargets() {
	o.mut.RLock()
	client := o.voteTargetsClient
	fallbackDenoms := append([]string(nil), o.cfg.FallbackDenoms...)
	hasVoteTargets := o.denomsFromVoteTargets
	o.mut.RUnlock()

	if client == nil {
		o.setDenoms(fallbackDenoms, false)
		return
	}

	denoms, err := client.VoteTargets()
	if err != nil {
		if hasVoteTargets {
			o.logger.Warn("failed to refresh vote targets; using last known denoms", "err", err)
			return
		}

		o.setDenoms(fallbackDenoms, false)
		o.logger.Warn("failed to refresh vote targets; using fallback config denoms", "err", err)
		return
	}

	o.setDenoms(denoms, true)
}

// setDenoms stores the effective vote-target denoms and whether they came from
// the vote-target client or the fallback config.
func (o *Runtime) setDenoms(denoms []string, fromVoteTargets bool) {
	o.mut.Lock()
	defer o.mut.Unlock()

	o.denoms = append([]string(nil), denoms...)
	o.denomsFromVoteTargets = fromVoteTargets
}

// recordMissingPrices records expected denoms missing from the latest aggregate prices.
func (o *Runtime) recordMissingPrices(ctx context.Context, denoms []string) {
	if len(denoms) == 0 {
		return
	}

	prices := types.PricesByDenom(o.resolver.GetPrices(), denoms)
	missing := make([]string, 0, len(denoms))
	for _, denom := range denoms {
		if _, ok := prices[denom]; !ok {
			missing = append(missing, denom)
		}
	}

	if len(missing) > 0 {
		o.logger.Warn(
			"oracle missing prices for active vote targets",
			"denoms", missing,
			"count", len(missing),
		)
	}

	oraclemetrics.RecordMissingPrices(ctx, missing)
}

// setLastSyncTime records when prices were last aggregated.
func (o *Runtime) setLastSyncTime(t time.Time) {
	o.mut.Lock()
	defer o.mut.Unlock()

	o.lastPriceSync = t
}
