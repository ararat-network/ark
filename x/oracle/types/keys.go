package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the oracle module
	ModuleName = "oracle"
	// StoreKey is the string store representation
	StoreKey = ModuleName
)

// Keys for oracle store
var (
	ParamsKey       = collections.NewPrefix(0)
	ExchangeRateKey = collections.NewPrefix(1)
	RewardWeightKey = collections.NewPrefix(2)
	AttendanceKey   = collections.NewPrefix(3)
	AccountingKey   = collections.NewPrefix(4)
	FeedsKey        = collections.NewPrefix(5)
)
