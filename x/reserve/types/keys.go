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
	MandateKey           = collections.NewPrefix(0)
	RecognitionPolicyKey = collections.NewPrefix(1)
	AllowanceUsedKey     = collections.NewPrefix(2)
	NextPositionIDKey    = collections.NewPrefix(3)
	OpenPositionsKey     = collections.NewPrefix(4)
	ClosedPositionsKey   = collections.NewPrefix(5)
	NextEntryIDKey       = collections.NewPrefix(6)
	LedgerKey            = collections.NewPrefix(7)
	ReversedReturnsKey   = collections.NewPrefix(8)
)
