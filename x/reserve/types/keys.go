package types

import "cosmossdk.io/collections"

const (
	ModuleName = "reserve"
	StoreKey   = ModuleName

	// StrategicReserveName is the custody account this module operates. The
	// name derives the module address, so it must never change after a genesis
	// anyone intends to keep.
	StrategicReserveName = "strategic_reserve"
)

var (
	ParamsKey            = collections.NewPrefix(0)
	MandateKey           = collections.NewPrefix(1)
	RecognitionPolicyKey = collections.NewPrefix(2)
	AllowanceUsedKey     = collections.NewPrefix(3)
	NextPositionIDKey    = collections.NewPrefix(4)
	OpenPositionsKey     = collections.NewPrefix(5)
	ClosedPositionsKey   = collections.NewPrefix(6)
	NextEntryIDKey       = collections.NewPrefix(7)
	LedgerKey            = collections.NewPrefix(8)
)
