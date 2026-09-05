package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Treasury errors.
var (
	ErrInvalidTaxMessage = errorsmod.Register(ModuleName, 1, "invalid tax message")
	ErrTaxOutOfRange     = errorsmod.Register(ModuleName, 2, "tax calculation is out of range")
)
