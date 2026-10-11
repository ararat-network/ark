package types

import (
	"google.golang.org/grpc/codes"

	errorsmod "cosmossdk.io/errors"
)

// Disbursement errors carry their gRPC code from registration, so a handler returns the keeper's
// error and the code follows it through every wrap. Every other failure in the module is a plain
// error: nothing branches on it. ErrPaymentRefused is the one the EndBlocker branches on: a send
// Bank refused at the payee is deferred, where any other failure fails the block.
var (
	ErrGrantNotFound       = errorsmod.RegisterWithGRPCCode(ModuleName, 1, codes.NotFound, "grant not found")
	ErrMemberNotFound      = errorsmod.RegisterWithGRPCCode(ModuleName, 2, codes.NotFound, "address holds no member grant")
	ErrBeneficiaryNotFound = errorsmod.RegisterWithGRPCCode(ModuleName, 3, codes.NotFound, "beneficiary not found")
	ErrPaymentRefused      = errorsmod.RegisterWithGRPCCode(ModuleName, 4, codes.FailedPrecondition, "payee refused the payment")
)
