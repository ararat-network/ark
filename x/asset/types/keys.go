package types

import (
	"cosmossdk.io/collections"
)

const (
	// ModuleName is the asset module name.
	ModuleName = "asset"
	// StoreKey is the asset module store key.
	StoreKey = ModuleName
)

var (
	ParamsKey            = collections.NewPrefix(0)
	AssetsKey            = collections.NewPrefix(1)
	SettlementPlansKey   = collections.NewPrefix(2)
	ResolutionRecordsKey = collections.NewPrefix(3)
	EmergencyMandateKey  = collections.NewPrefix(4)
	EmergencyActionsKey  = collections.NewPrefix(5)
)
