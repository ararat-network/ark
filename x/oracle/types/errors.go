// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/oracle/types/errors.go.
// Modified for Ark: chain-specific error definitions.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Oracle Errors
var (
	ErrInvalidExchangeRate             = errorsmod.Register(ModuleName, 1, "invalid exchange rate")
	ErrUnknownDenom                    = errorsmod.Register(ModuleName, 2, "unknown denom")
	ErrStaleExchangeRate               = errorsmod.Register(ModuleName, 3, "stale exchange rate")
	ErrConversionOutOfRange            = errorsmod.Register(ModuleName, 4, "conversion result is out of range")
	ErrFeedTransitionPending           = errorsmod.Register(ModuleName, 5, "conflicting feed transition is pending")
	ErrFeedReferenced                  = errorsmod.Register(ModuleName, 6, "feed is referenced by a consumer")
	ErrFeedNotFound                    = errorsmod.Register(ModuleName, 7, "no active or in-flight feed for denom")
	ErrInvalidReferenceDenom           = errorsmod.Register(ModuleName, 8, "invalid protocol reference denom")
	ErrReferenceDenomRebaseUnavailable = errorsmod.Register(ModuleName, 9, "protocol reference denom cannot be rebased")
	ErrInvalidMaxExchangeRateAge       = errorsmod.Register(ModuleName, 10, "invalid max exchange rate age")
)
