package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Oracle Errors
var (
	ErrInvalidExchangeRate = sdkerrors.Register(ModuleName, 1, "invalid exchange rate")
	ErrVerificationFailed  = sdkerrors.Register(ModuleName, 2, "hash verification failed")
	ErrUnknownDenom        = sdkerrors.Register(ModuleName, 3, "unknown denom")
	ErrStaleExchangeRate   = sdkerrors.Register(ModuleName, 4, "stale exchange rate")
)
