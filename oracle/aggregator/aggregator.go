package aggregator

import (
	"context"
	"math/big"
	"slices"
	"sync"

	"noah/oracle"
	oraclemetrics "noah/oracle/metrics"
	"noah/oracle/types"
)

var _ oracle.PriceAggregator = &MedianAggregator{}

type MedianAggregator struct {
	mtx sync.Mutex

	providerPrices map[string]types.Prices
	finalPrices    types.Prices
}

// NewMedianAggregator returns a new Median aggregator.
func NewMedianAggregator() *MedianAggregator {
	return &MedianAggregator{
		providerPrices: make(map[string]types.Prices),
		finalPrices:    make(types.Prices),
	}
}

// SetProviderPrices updates the data aggregator with the given provider and data.
func (m *MedianAggregator) SetProviderPrices(provider string, data types.Prices) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	prices := make(types.Prices)
	for denom, price := range data {
		if price == nil {
			continue
		}
		prices[denom] = new(big.Float).Copy(price)
	}

	m.providerPrices[provider] = prices
}

// AggregatePrices inputs the aggregated prices from all providers and computes
// the median price for each asset.
func (m *MedianAggregator) AggregatePrices() {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	ctx := context.Background()

	// Aggregate prices across all providers for each asset.
	pricesByAsset := make(map[string][]*big.Float)
	for provider, providerPrices := range m.providerPrices {
		for cp, price := range providerPrices {
			// Only include prices that are not nil
			if price == nil {
				continue
			}

			floatPrice, _ := price.Float64()
			oraclemetrics.RecordProviderPrice(ctx, provider, cp, floatPrice)
			oraclemetrics.RecordProviderContribution(ctx, provider, cp, true)

			// Initialise the asset array if it doesn't exist
			if _, ok := pricesByAsset[cp]; !ok {
				pricesByAsset[cp] = make([]*big.Float, 0)
			}

			pricesByAsset[cp] = append(pricesByAsset[cp], price)
		}
	}

	// Iterate through all assets and compute the median price
	medianPrices := make(types.Prices)
	for cp, prices := range pricesByAsset {
		if len(prices) == 0 {
			continue
		}

		medianPrices[cp] = calculateMedian(prices)
		floatPrice, _ := medianPrices[cp].Float64()
		oraclemetrics.RecordProviderCount(ctx, cp, len(prices))
		oraclemetrics.RecordAggregatePrice(ctx, cp, floatPrice)
	}
	m.finalPrices = medianPrices
}

// GetPrices returns the aggregated data the aggregator has.
func (m *MedianAggregator) GetPrices() types.Prices {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	finalPrices := make(types.Prices)
	for denom, price := range m.finalPrices {
		finalPrices[denom] = new(big.Float).Copy(price)
	}

	return finalPrices
}

// Reset resets the data aggregator for all providers.
func (m *MedianAggregator) Reset() {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.providerPrices = make(map[string]types.Prices)
}

// calculateMedian calculates the median from a list of big.Float. Returns an
// average if the number of values is even.
func calculateMedian(values []*big.Float) *big.Float {
	if len(values) == 0 {
		return nil
	}

	slices.SortFunc(values, func(a, b *big.Float) int {
		return a.Cmp(b)
	})

	mid := len(values) / 2
	if len(values)%2 == 1 {
		return new(big.Float).Copy(values[mid])
	}

	median := new(big.Float).Add(values[mid-1], values[mid])
	return median.Quo(median, new(big.Float).SetUint64(2))
}
