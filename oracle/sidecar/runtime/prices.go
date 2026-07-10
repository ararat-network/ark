package runtime

import (
	"context"
	"math/big"
	"time"

	oraclemetrics "ark/oracle/sidecar/metrics"
	"ark/oracle/sidecar/providers/base"
	"ark/oracle/sidecar/resolver"
	"ark/oracle/sidecar/types"
)

// updatePriceSnapshot serialises vote-target sync, provider cache reads,
// resolution, missing-denom zero-filling, and snapshot commit with config and
// lifecycle transitions under updateMu.
func (r *Runtime) updatePriceSnapshot(ctx context.Context) {
	r.logger.Debug("updating price snapshot")

	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	r.syncVoteTargetsLocked()

	r.mut.RLock()
	denoms := append([]string(nil), r.denoms...)
	resolverCfg := r.cfg.Resolver
	maxPriceAges := make(map[string]time.Duration, len(r.cfg.Providers))
	for name, providerCfg := range r.cfg.Providers {
		maxPriceAges[name] = providerCfg.MaxPriceAge
	}
	r.mut.RUnlock()

	now := time.Now().UTC()
	providerPrices := make(map[string]types.Prices, len(maxPriceAges))
	for name, maxPriceAge := range maxPriceAges {
		managed, ok := r.providers[name]
		if !ok {
			continue
		}
		providerPrices[name] = r.freshProviderPrices(managed.provider, now, maxPriceAge)
	}
	r.logger.Debug("collected cached provider prices")

	resolvedPrices := resolver.ResolvePrices(ctx, resolverCfg, providerPrices, denoms, now)
	prices := types.PricesByDenom(resolvedPrices, denoms)
	r.recordMissingPrices(ctx, denoms, prices)
	for _, denom := range denoms {
		if _, ok := prices[denom]; !ok {
			prices[denom] = new(big.Float)
		}
	}
	r.commitPriceSnapshot(prices, now)
	oraclemetrics.RecordOracleTick(ctx)
}

// freshProviderPrices returns one provider's cached prices that are fresh enough
// for the current aggregation tick.
func (r *Runtime) freshProviderPrices(
	provider *base.Provider,
	now time.Time,
	maxPriceAge time.Duration,
) types.Prices {
	name := provider.Name()
	transport := provider.Type()
	r.logger.Debug(
		"retrieving prices",
		"provider", name,
		"transport", transport,
	)

	prices := provider.GetPrices()
	if prices == nil {
		r.logger.Debug(
			"provider returned nil prices",
			"provider", name,
			"transport", transport,
		)

		return nil
	}

	freshPrices := make(types.Prices)
	for pair, result := range prices {
		age := now.Sub(result.Timestamp)
		if age > maxPriceAge {
			r.logger.Debug(
				"skipping price",
				"provider", name,
				"transport", transport,
				"pair", pair,
				"age", age,
			)

			continue
		}

		r.logger.Debug(
			"adding price",
			"provider", name,
			"transport", transport,
			"pair", pair,
			"price", result.Price,
			"age", age,
		)
		freshPrices[pair] = result.Price
	}

	r.logger.Debug(
		"provider prices collected",
		"provider", name,
		"transport", transport,
		"prices", len(freshPrices),
	)
	return freshPrices
}

// recordMissingPrices records expected denoms missing from the latest aggregate
// prices before the public snapshot is zero-filled.
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
