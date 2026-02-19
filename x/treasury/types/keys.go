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

// BurnModuleName is special purpose module name to perform burn coins
// burn address = terra1sk06e3dyexuq4shw77y3dsv480xv42mq73anxu
const BurnModuleName = "burn"

var (
	// Keys for treasury store
	ParamsKey               = collections.NewPrefix(0)
	TaxRateKey              = collections.NewPrefix(1)
	RewardWeightKey         = collections.NewPrefix(2)
	TaxCapsKey              = collections.NewPrefix(3)
	EpochTaxProceedsKey     = collections.NewPrefix(4)
	EpochInitialIssuanceKey = collections.NewPrefix(5)
	EpochStatesKey          = collections.NewPrefix(6)
	BurnTaxExemptionsKey    = collections.NewPrefix(7)
)
