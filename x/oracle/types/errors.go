package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Oracle Errors
var (
	ErrInvalidExchangeRate             = sdkerrors.Register(ModuleName, 1, "invalid exchange rate")
	ErrUnknownDenom                    = sdkerrors.Register(ModuleName, 2, "unknown denom")
	ErrStaleExchangeRate               = sdkerrors.Register(ModuleName, 3, "stale exchange rate")
	ErrConversionOutOfRange            = sdkerrors.Register(ModuleName, 4, "conversion result is out of range")
	ErrFeedTransitionPending           = sdkerrors.Register(ModuleName, 5, "conflicting feed transition is pending")
	ErrFeedReferenced                  = sdkerrors.Register(ModuleName, 6, "feed is referenced by a consumer")
	ErrFeedNotFound                    = sdkerrors.Register(ModuleName, 7, "no active or in-flight feed for denom")
	ErrInvalidReferenceDenom           = sdkerrors.Register(ModuleName, 8, "invalid protocol reference denom")
	ErrReferenceDenomRebaseUnavailable = sdkerrors.Register(ModuleName, 9, "protocol reference denom cannot be rebased")
	ErrInvalidMaxExchangeRateAge       = sdkerrors.Register(ModuleName, 10, "invalid max exchange rate age")
)
