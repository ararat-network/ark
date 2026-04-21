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
	ParamsKey           = collections.NewPrefix(0)
	FeederDelegationKey = collections.NewPrefix(1)
	ExchangeRateKey     = collections.NewPrefix(2)
	MissCountKey        = collections.NewPrefix(3)
	PrevoteKey          = collections.NewPrefix(4)
	VoteKey             = collections.NewPrefix(5)
	TobinTaxKey         = collections.NewPrefix(6)
)
