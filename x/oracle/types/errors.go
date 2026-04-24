package types

import (
	"fmt"

	sdkerrors "cosmossdk.io/errors"
)

// Oracle Errors
var (
	ErrInvalidExchangeRate   = sdkerrors.Register(ModuleName, 2, "invalid exchange rate")
	ErrNoPrevote             = sdkerrors.Register(ModuleName, 3, "no prevote")
	ErrNoVote                = sdkerrors.Register(ModuleName, 4, "no vote")
	ErrNoVotingPermission    = sdkerrors.Register(ModuleName, 5, "unauthorised voter")
	ErrInvalidHash           = sdkerrors.Register(ModuleName, 6, "invalid hash")
	ErrInvalidHashLength     = sdkerrors.Register(ModuleName, 7, fmt.Sprintf("invalid hash length; should equal %d", TruncatedHashSize))
	ErrVerificationFailed    = sdkerrors.Register(ModuleName, 8, "hash verification failed")
	ErrRevealPeriodMissMatch = sdkerrors.Register(ModuleName, 9, "reveal period of submitted vote do not match with registered prevote")
	ErrInvalidSaltLength     = sdkerrors.Register(ModuleName, 10, "invalid salt length; should be 1~4")
	ErrUnknownDenom          = sdkerrors.Register(ModuleName, 14, "unknown denom")
)
