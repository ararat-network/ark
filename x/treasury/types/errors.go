package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Treasury errors.
var (
	ErrInvalidTaxMessage = sdkerrors.Register(ModuleName, 1, "invalid tax message")
	ErrTaxOutOfRange     = sdkerrors.Register(ModuleName, 2, "tax calculation is out of range")
)
