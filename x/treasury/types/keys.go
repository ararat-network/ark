package types

import (
	"cosmossdk.io/collections"
)

const (
	// ModuleName is the module name constant used in many places
	ModuleName = "treasury"
	// StoreKey is the string store representation
	StoreKey = ModuleName
)

var (
	// Keys for treasury store
	ParamsKey               = collections.NewPrefix(0)
	TaxRateKey              = collections.NewPrefix(1)
	RewardWeightKey         = collections.NewPrefix(2)
	TaxCapsKey              = collections.NewPrefix(3)
	EpochTaxProceedsKey     = collections.NewPrefix(4)
	EpochInitialIssuanceKey = collections.NewPrefix(5)
	EpochStatesKey          = collections.NewPrefix(6)
)
