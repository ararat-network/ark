package oracle

import (
	"context"
	"time"

	oraclemetrics "noah/oracle/metrics"
	"noah/oracle/providers/base"
	"noah/oracle/types"
)

// fetchAllPrices snapshots active providers, collects their latest prices, and updates aggregation state.
func (o *Oracle) fetchAllPrices() {
	o.logger.Debug("starting price fetch loop")
	defer func() {
		if r := recover(); r != nil {
			o.logger.Error("fetchAllPrices tick panicked", "error", r)
		}
	}()

	o.aggregator.Reset()

	providers, maxPriceAge, denoms := o.priceFetchSnapshot()
	for _, provider := range providers {
		o.fetchPrices(provider, maxPriceAge)
	}

	o.logger.Debug("oracle fetched prices from providers")

	o.aggregator.AggregatePrices()
	o.recordMissingPrices(denoms)
	o.setLastSyncTime(time.Now().UTC())
	oraclemetrics.RecordOracleTick(context.Background())
}

// fetchPrices copies fresh prices from one provider into the aggregator.
func (o *Oracle) fetchPrices(provider *base.Provider, maxPriceAge time.Duration) {
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
	for denom, result := range prices {
		// If the price is older than maxPriceAge, skip it.
		diff := time.Now().UTC().Sub(result.Timestamp)
		if diff > maxPriceAge {
			o.logger.Debug(
				"skipping price",
				"provider", provider.Name(),
				"data handler type", provider.Type(),
				"denom", denom,
				"diff", diff,
			)

			continue
		}

		o.logger.Debug(
			"adding price",
			"provider", provider.Name(),
			"data handler type", provider.Type(),
			"denom", denom,
			"price", result.Price,
			"diff", diff,
		)
		timeFilteredPrices[denom] = result.Price
	}

	o.logger.Debug("provider returned prices",
		"provider", provider.Name(),
		"data handler type", provider.Type(),
		"prices", len(prices),
	)
	o.aggregator.SetProviderPrices(provider.Name(), timeFilteredPrices)
}

// priceFetchSnapshot returns the providers, freshness window, and expected denoms for one fetch tick.
func (o *Oracle) priceFetchSnapshot() ([]*base.Provider, time.Duration, []string) {
	o.mut.RLock()
	defer o.mut.RUnlock()

	providers := make([]*base.Provider, 0, len(o.providers))
	for _, provider := range o.providers {
		providers = append(providers, provider)
	}

	return providers, o.cfg.MaxPriceAge, append([]string(nil), o.cfg.Denoms...)
}

// recordMissingPrices records expected denoms missing from the latest aggregate prices.
func (o *Oracle) recordMissingPrices(denoms []string) {
	if len(denoms) == 0 {
		return
	}

	prices := o.aggregator.GetPrices()
	missing := make([]string, 0, len(denoms))
	for _, denom := range denoms {
		if _, ok := prices[denom]; !ok {
			missing = append(missing, denom)
		}
	}

	oraclemetrics.RecordMissingPrices(context.Background(), missing)
}

// setLastSyncTime records when prices were last aggregated.
func (o *Oracle) setLastSyncTime(t time.Time) {
	o.mut.Lock()
	defer o.mut.Unlock()

	o.lastPriceSync = t
}
