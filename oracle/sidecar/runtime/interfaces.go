package runtime

import (
	"context"

	"noah/oracle/sidecar/chainstate"
	"noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/types"
)

// PriceResolver computes oracle prices from provider observations. Implementations
// should be safe for concurrent use.
type PriceResolver interface {
	SetProviderPrices(provider string, prices types.Prices)
	ResolvePrices(denoms []string)
	GetPrices() types.Prices
	Update(resolver.Config)
	Reset()
}

// ChainStateClient fetches the current on-chain oracle vote targets.
type ChainStateClient interface {
	Start(context.Context) error
	Stop()
	Update(chainstate.Config)
	VoteTargets() ([]string, error)
}
