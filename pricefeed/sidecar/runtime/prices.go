package runtime

import (
	"context"
	"time"

	sidecarmetrics "github.com/ararat-network/ark/pricefeed/sidecar/metrics"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	"github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// updatePriceSnapshot serialises feed sync, provider cache reads, resolution,
// and snapshot commit with config and lifecycle transitions under updateMu.
// Missing feeds remain absent from the committed snapshot.
func (r *Runtime) updatePriceSnapshot(ctx context.Context) {
	r.logger.Debug("updating price snapshot")

	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	r.syncFeedsLocked()
	r.initialiseMetrics(ctx)

	r.mut.RLock()
	feeds := append([]string(nil), r.feeds...)
	resolverCfg := r.cfg.Resolver
	freshness := make(map[string]providerFreshness, len(r.cfg.Providers))
	for name, providerCfg := range r.cfg.Providers {
		freshness[name] = providerFreshness{
			maxPriceAge:     providerCfg.MaxPriceAge,
			maxUnchangedAge: providerCfg.MaxUnchangedAge,
		}
	}
	r.mut.RUnlock()

	now := time.Now().UTC()
	providerPrices := make(map[string]types.Prices, len(freshness))
	for name, window := range freshness {
		managed, ok := r.providers[name]
		if !ok {
			continue
		}
		providerPrices[name] = r.freshProviderPrices(ctx, managed.provider, now, window)
	}
	r.logger.Debug("collected cached provider prices")

	resolvedPrices := resolver.ResolvePrices(ctx, resolverCfg, providerPrices, feeds, now)
	prices := types.PricesByFeed(resolvedPrices, feeds)
	r.recordMissingPrices(ctx, feeds, prices)
	r.commitPriceSnapshot(prices, now)
	sidecarmetrics.RecordTick(ctx)
}

// providerFreshness holds message-age and unchanged-price limits. maxPriceAge bounds the latest
// refresh; maxUnchangedAge bounds extension beyond the last real observation, with zero allowing
// none.
type providerFreshness struct {
	maxPriceAge     time.Duration
	maxUnchangedAge time.Duration
}

// freshProviderPrices filters cached observations through both freshness windows.
func (r *Runtime) freshProviderPrices(
	ctx context.Context,
	provider *base.Provider,
	now time.Time,
	window providerFreshness,
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
		if age > window.maxPriceAge {
			r.logger.Debug(
				"skipping price",
				"provider", name,
				"transport", transport,
				"pair", pair,
				"age", age,
			)
			sidecarmetrics.RecordSkippedSample(ctx, name, pair.String(), sidecarmetrics.SkipReasonStale)

			continue
		}
		// An unchanged refresh moved Timestamp but not LastObserved; this is
		// the bound on how far the two may drift.
		observedAge := now.Sub(result.LastObserved)
		limit := window.maxUnchangedAge
		if limit == 0 {
			limit = window.maxPriceAge
		}
		if observedAge > limit {
			r.logger.Debug(
				"skipping price past its unchanged bound",
				"provider", name,
				"transport", transport,
				"pair", pair,
				"observed_age", observedAge,
				"limit", limit,
			)
			sidecarmetrics.RecordSkippedSample(ctx, name, pair.String(), sidecarmetrics.SkipReasonUnchanged)

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

// recordMissingPrices records expected feeds missing from the latest aggregate
// prices.
func (r *Runtime) recordMissingPrices(ctx context.Context, feeds []string, prices types.FeedPrices) {
	if len(feeds) == 0 {
		return
	}

	missing := make([]string, 0, len(feeds))
	for _, denom := range feeds {
		if _, ok := prices[denom]; !ok {
			missing = append(missing, denom)
		}
	}

	if len(missing) > 0 {
		r.logger.Warn(
			"oracle missing prices for active feeds",
			"feeds", missing,
			"count", len(missing),
		)
	}

	sidecarmetrics.RecordMissingPrices(ctx, missing)
}

// commitPriceSnapshot stores a runtime-owned price snapshot from one aggregation
// tick. The caller must not mutate prices after commit.
func (r *Runtime) commitPriceSnapshot(prices types.FeedPrices, timestamp time.Time) {
	r.mut.Lock()
	defer r.mut.Unlock()

	r.priceSnapshot = types.PriceSnapshot{
		Prices:    prices,
		Timestamp: timestamp,
	}
}
