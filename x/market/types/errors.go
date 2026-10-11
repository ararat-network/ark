package types

import (
	"google.golang.org/grpc/codes"

	errorsmod "cosmossdk.io/errors"
)

// Market errors carry their gRPC code from registration, so a query handler returns the keeper's
// error and the code follows it through every wrap.
var (
	ErrRecursiveSwap        = errorsmod.RegisterWithGRPCCode(ModuleName, 1, codes.InvalidArgument, "recursive swap")
	ErrZeroSwapCoin         = errorsmod.RegisterWithGRPCCode(ModuleName, 2, codes.InvalidArgument, "zero swap coin")
	ErrMinimumReceiveNotMet = errorsmod.RegisterWithGRPCCode(ModuleName, 3, codes.FailedPrecondition, "minimum receive amount not met")
	ErrArithmeticOutOfRange = errorsmod.RegisterWithGRPCCode(ModuleName, 4, codes.OutOfRange, "market arithmetic is out of range")
	ErrIneligibleAsset      = errorsmod.RegisterWithGRPCCode(ModuleName, 5, codes.FailedPrecondition, "asset is not eligible for conversion")
	ErrNoActiveSettlement   = errorsmod.RegisterWithGRPCCode(ModuleName, 6, codes.FailedPrecondition, "asset has no active settlement plan")
	ErrTobinOverrideMissing = errorsmod.RegisterWithGRPCCode(ModuleName, 7, codes.NotFound, "tobin tax override is not set")
	ErrMaximumOfferExceeded = errorsmod.RegisterWithGRPCCode(ModuleName, 8, codes.FailedPrecondition, "maximum offer exceeded")
	ErrInvalidOrder         = errorsmod.RegisterWithGRPCCode(ModuleName, 9, codes.InvalidArgument, "invalid order")
)
