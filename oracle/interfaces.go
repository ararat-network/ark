package oracle

import (
	"context"
	"time"

	"noah/oracle/types"
)

// Oracle defines the expected interface for an oracle. It is consumed by the oracle server.
type Oracle interface {
	IsRunning() bool
	GetLastSyncTime() time.Time
	GetPrices() types.Prices
	Start(ctx context.Context) error
	Stop()
}

// PriceAggregator is an interface for aggregating prices from multiple providers. Implementations of PriceAggregator
// should be made safe for concurrent use.
type PriceAggregator interface {
	SetProviderPrices(provider string, prices types.Prices)
	AggregatePrices()
	GetPrices() types.Prices
	Reset()
}

// generalProvider is an interface for a provider that implements the base provider.
type generalProvider interface {
	// Start starts the provider.
	Start(ctx context.Context) error
	// Name is the provider's name.
	Name() string
}
