package oracle

import (
	"noah/oracle/types"
)

// PriceAggregator is an interface for aggregating prices from multiple providers. Implementations of PriceAggregator
// should be made safe for concurrent use.
type PriceAggregator interface {
	SetProviderPrices(provider string, prices types.Prices)
	AggregatePrices()
	GetPrices() types.Prices
	Reset()
}
