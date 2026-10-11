package types

import (
	"google.golang.org/grpc/codes"

	errorsmod "cosmossdk.io/errors"
)

// Treasury errors carry their gRPC code from registration, so a query handler returns the keeper's
// error and the code follows it through every wrap.
var (
	ErrInvalidTaxMessage        = errorsmod.RegisterWithGRPCCode(ModuleName, 1, codes.InvalidArgument, "invalid tax message")
	ErrTaxOutOfRange            = errorsmod.RegisterWithGRPCCode(ModuleName, 2, codes.OutOfRange, "tax calculation is out of range")
	ErrConversionFactorNotFound = errorsmod.RegisterWithGRPCCode(ModuleName, 3, codes.NotFound, "no conversion factor for denom")
)
