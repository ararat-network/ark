package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the market module
	ModuleName = "oracle"
	// StoreKey is the string store representation
	StoreKey = ModuleName
)

// Keys for oracle store
var (
	ParamsKey                       = collections.NewPrefix(0)
	FeederDelegationKey             = collections.NewPrefix(1)
	ExchangeRateKey                 = collections.NewPrefix(2)
	MissCounterKey                  = collections.NewPrefix(3)
	AggregateExchangeRatePrevoteKey = collections.NewPrefix(4)
	AggregateExchangeRateVoteKey    = collections.NewPrefix(5)
	TobinTaxKey                     = collections.NewPrefix(6)
)
