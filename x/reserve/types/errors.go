package types

import (
	"google.golang.org/grpc/codes"

	errorsmod "cosmossdk.io/errors"
)

// Reserve errors carry their gRPC code from registration, so a handler returns the keeper's error
// and the code follows it through every wrap. Every other failure in the module is a plain error:
// nothing branches on it.
var (
	ErrPositionNotFound = errorsmod.RegisterWithGRPCCode(ModuleName, 1, codes.NotFound, "position not found")
)
