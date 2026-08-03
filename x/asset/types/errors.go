package types

import sdkerrors "cosmossdk.io/errors"

// Asset errors.
var (
	ErrAssetNotFound            = sdkerrors.Register(ModuleName, 1, "asset not found")
	ErrAssetAlreadyExists       = sdkerrors.Register(ModuleName, 2, "asset already exists")
	ErrAssetVersionMismatch     = sdkerrors.Register(ModuleName, 3, "asset version mismatch")
	ErrInvalidAssetTransition   = sdkerrors.Register(ModuleName, 4, "invalid asset lifecycle transition")
	ErrAssetSupplyNotZero       = sdkerrors.Register(ModuleName, 5, "asset supply is not zero")
	ErrSettlementPlanNotFound   = sdkerrors.Register(ModuleName, 6, "settlement plan not found")
	ErrAssetNotPriceable        = sdkerrors.Register(ModuleName, 7, "asset is not priceable")
	ErrEmergencyMandateInactive = sdkerrors.Register(ModuleName, 8, "emergency mandate does not authorise this action")
	ErrEmergencyActionConsumed  = sdkerrors.Register(ModuleName, 9, "emergency action already consumed this term")
)
