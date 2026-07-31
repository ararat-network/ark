package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Market errors
var (
	ErrRecursiveSwap        = sdkerrors.Register(ModuleName, 1, "recursive swap")
	ErrNoEffectivePrice     = sdkerrors.Register(ModuleName, 2, "no price registered with oracle")
	ErrZeroSwapCoin         = sdkerrors.Register(ModuleName, 3, "zero swap coin")
	ErrMinimumReceiveNotMet = sdkerrors.Register(ModuleName, 4, "minimum receive amount not met")
	ErrArithmeticOutOfRange = sdkerrors.Register(ModuleName, 5, "market arithmetic is out of range")
	ErrIneligibleAsset      = sdkerrors.Register(ModuleName, 6, "asset is not eligible for conversion")
	ErrNoActiveSettlement   = sdkerrors.Register(ModuleName, 7, "asset has no active settlement plan")
	ErrTobinOverrideMissing = sdkerrors.Register(ModuleName, 8, "tobin tax override is not set")
)
