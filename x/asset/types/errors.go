package types

import (
	"google.golang.org/grpc/codes"

	errorsmod "cosmossdk.io/errors"
)

// Asset errors carry their gRPC code from registration, so a handler returns the keeper's error and
// the code follows it through every wrap.
var (
	ErrAssetNotFound               = errorsmod.RegisterWithGRPCCode(ModuleName, 1, codes.NotFound, "asset not found")
	ErrAssetAlreadyExists          = errorsmod.RegisterWithGRPCCode(ModuleName, 2, codes.AlreadyExists, "asset already exists")
	ErrAssetVersionMismatch        = errorsmod.RegisterWithGRPCCode(ModuleName, 3, codes.FailedPrecondition, "asset version mismatch")
	ErrInvalidAssetTransition      = errorsmod.RegisterWithGRPCCode(ModuleName, 4, codes.FailedPrecondition, "invalid asset lifecycle transition")
	ErrAssetSupplyNotZero          = errorsmod.RegisterWithGRPCCode(ModuleName, 5, codes.FailedPrecondition, "asset supply is not zero")
	ErrSettlementPlanNotFound      = errorsmod.RegisterWithGRPCCode(ModuleName, 6, codes.NotFound, "settlement plan not found")
	ErrAssetNotPriceable           = errorsmod.RegisterWithGRPCCode(ModuleName, 7, codes.FailedPrecondition, "asset is not priceable")
	ErrEmergencyMandateInactive    = errorsmod.RegisterWithGRPCCode(ModuleName, 8, codes.PermissionDenied, "emergency mandate does not authorise this action")
	ErrEmergencySuspensionConsumed = errorsmod.RegisterWithGRPCCode(ModuleName, 9, codes.FailedPrecondition, "emergency suspension already consumed this term")
)
