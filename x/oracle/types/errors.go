// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/oracle/types/errors.go.
// Modified for Ark: chain-specific error definitions.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package types

import (
	"errors"

	"google.golang.org/grpc/codes"

	errorsmod "cosmossdk.io/errors"
)

// Oracle errors carry their gRPC code from registration, so a query handler returns the keeper's
// error and the code follows it through every wrap.
var (
	ErrInvalidExchangeRate             = errorsmod.RegisterWithGRPCCode(ModuleName, 1, codes.InvalidArgument, "invalid exchange rate")
	ErrUnknownDenom                    = errorsmod.RegisterWithGRPCCode(ModuleName, 2, codes.NotFound, "unknown denom")
	ErrStaleExchangeRate               = errorsmod.RegisterWithGRPCCode(ModuleName, 3, codes.FailedPrecondition, "stale exchange rate")
	ErrConversionOutOfRange            = errorsmod.RegisterWithGRPCCode(ModuleName, 4, codes.OutOfRange, "conversion result is out of range")
	ErrFeedTransitionPending           = errorsmod.RegisterWithGRPCCode(ModuleName, 5, codes.FailedPrecondition, "conflicting feed transition is pending")
	ErrFeedReferenced                  = errorsmod.RegisterWithGRPCCode(ModuleName, 6, codes.FailedPrecondition, "feed is referenced by a consumer")
	ErrFeedNotFound                    = errorsmod.RegisterWithGRPCCode(ModuleName, 7, codes.NotFound, "no active or in-flight feed for denom")
	ErrInvalidReferenceDenom           = errorsmod.RegisterWithGRPCCode(ModuleName, 8, codes.InvalidArgument, "invalid protocol reference denom")
	ErrReferenceDenomRebaseUnavailable = errorsmod.RegisterWithGRPCCode(ModuleName, 9, codes.FailedPrecondition, "protocol reference denom cannot be rebased")
)

// IsRateUnavailable reports a failure of the rate inputs rather than of the caller's own
// arithmetic: no rate, a stale or invalid one, or a conversion the rates cannot represent. A
// consumer that falls back on bad inputs branches on it instead of naming the oracle's errors.
func IsRateUnavailable(err error) bool {
	return errors.Is(err, ErrUnknownDenom) ||
		errors.Is(err, ErrStaleExchangeRate) ||
		errors.Is(err, ErrInvalidExchangeRate) ||
		errors.Is(err, ErrConversionOutOfRange)
}
