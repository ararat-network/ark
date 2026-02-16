package types

import "cosmossdk.io/collections"

const (
	// ModuleName is the name of the market module
	ModuleName = "market"
	// StoreKey is the string store representation
	StoreKey = ModuleName
	// RouterKey is the msg router key for the staking module
	RouterKey = ModuleName
)

// Keys for market store
var (
	ParamsKey        = collections.NewPrefix(0)
	NoahPoolDeltaKey = collections.NewPrefix(1)
)
