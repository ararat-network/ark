package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Market errors
var (
	ErrRecursiveSwap        = errorsmod.Register(ModuleName, 1, "recursive swap")
	ErrNoEffectivePrice     = errorsmod.Register(ModuleName, 2, "no price registered with oracle")
	ErrZeroSwapCoin         = errorsmod.Register(ModuleName, 3, "zero swap coin")
	ErrMinimumReceiveNotMet = errorsmod.Register(ModuleName, 4, "minimum receive amount not met")
	ErrArithmeticOutOfRange = errorsmod.Register(ModuleName, 5, "market arithmetic is out of range")
	ErrIneligibleAsset      = errorsmod.Register(ModuleName, 6, "asset is not eligible for conversion")
	ErrNoActiveSettlement   = errorsmod.Register(ModuleName, 7, "asset has no active settlement plan")
	ErrTobinOverrideMissing = errorsmod.Register(ModuleName, 8, "tobin tax override is not set")
)
