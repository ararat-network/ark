package types

import (
	sdkerrors "cosmossdk.io/errors"
)

// Market errors
var (
	ErrRecursiveSwap    = sdkerrors.Register(ModuleName, 2, "recursive swap")
	ErrNoEffectivePrice = sdkerrors.Register(ModuleName, 3, "no price registered with oracle")
	ErrZeroSwapCoin     = sdkerrors.Register(ModuleName, 4, "zero swap coin")
)
