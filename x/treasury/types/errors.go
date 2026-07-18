package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Treasury errors.
var (
	ErrInvalidTaxMessage = sdkerrors.Register(ModuleName, 1, "invalid tax message")
	ErrTaxCapUnavailable = sdkerrors.Register(ModuleName, 2, "tax cap is unavailable")
	ErrTaxOutOfRange     = sdkerrors.Register(ModuleName, 3, "tax calculation is out of range")
)
