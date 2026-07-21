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
	ParamsKey       = collections.NewPrefix(0)
	ExchangeRateKey = collections.NewPrefix(1)
	RewardWeightKey = collections.NewPrefix(2)
	MissCountKey    = collections.NewPrefix(3)
	VoteTargetsKey  = collections.NewPrefix(4)
	AccountingKey   = collections.NewPrefix(5)
)
