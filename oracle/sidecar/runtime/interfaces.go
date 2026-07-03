package runtime

import (
	"noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/types"
)

// PriceResolver computes oracle prices from provider observations. Implementations
// should be safe for concurrent use.
type PriceResolver interface {
	SetProviderPrices(provider string, prices types.Prices)
	ResolvePrices(denoms []string)
	GetPrices() types.Prices
	UpdateConfig(resolver.Config) error
	Reset()
}

// VoteTargetsClient fetches the current on-chain oracle vote targets.
type VoteTargetsClient interface {
	VoteTargets() ([]string, error)
}
