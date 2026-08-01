package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Treasury errors.
var (
	ErrInvalidTaxMessage = sdkerrors.Register(ModuleName, 1, "invalid tax message")
	// Code 2 was ErrTaxCapUnavailable, retired when a missing cap became an
	// untaxed denomination rather than a rejected transaction. Codes are not
	// reused: an old client mapping 2 must not resolve to a new meaning.
	ErrTaxOutOfRange = sdkerrors.Register(ModuleName, 3, "tax calculation is out of range")
)
