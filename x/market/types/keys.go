package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the market module
	ModuleName = "market"
	// StoreKey is the string store representation
	StoreKey = ModuleName
)

// Keys for market store
var (
	ParamsKey       = collections.NewPrefix(0)
	ArkPoolDeltaKey = collections.NewPrefix(1)
)
