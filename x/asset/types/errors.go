package types

import errorsmod "cosmossdk.io/errors"

// Asset errors.
var (
	ErrAssetNotFound               = errorsmod.Register(ModuleName, 1, "asset not found")
	ErrAssetAlreadyExists          = errorsmod.Register(ModuleName, 2, "asset already exists")
	ErrAssetVersionMismatch        = errorsmod.Register(ModuleName, 3, "asset version mismatch")
	ErrInvalidAssetTransition      = errorsmod.Register(ModuleName, 4, "invalid asset lifecycle transition")
	ErrAssetSupplyNotZero          = errorsmod.Register(ModuleName, 5, "asset supply is not zero")
	ErrSettlementPlanNotFound      = errorsmod.Register(ModuleName, 6, "settlement plan not found")
	ErrAssetNotPriceable           = errorsmod.Register(ModuleName, 7, "asset is not priceable")
	ErrEmergencyMandateInactive    = errorsmod.Register(ModuleName, 8, "emergency mandate does not authorise this action")
	ErrEmergencySuspensionConsumed = errorsmod.Register(ModuleName, 9, "emergency suspension already consumed this term")
)
