package ante

import (
	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// walkAuthzExec visits leaf messages inside bounded authz MsgExec nesting. All message policies
// share this traversal at both the ante and execution-router boundaries. MsgExec wrappers
// themselves are not visited.
func walkAuthzExec(cdc codec.Codec, msg sdk.Msg, depth int, visit func(sdk.Msg) error) error {
	// The decoder's unpack-depth cap is the recursion bound: a signed
	// transaction nested this deep cannot decode, so the check binds only
	// for messages arriving off the tx path through the router.
	if depth >= codectypes.MaxUnpackAnyRecursionDepth {
		return errorsmod.Wrap(errortypes.ErrInvalidRequest, "too many nested authz exec messages")
	}
	exec, ok := msg.(*authz.MsgExec)
	if !ok {
		return visit(msg)
	}
	for _, wrapped := range exec.Msgs {
		var inner sdk.Msg
		if err := cdc.UnpackAny(wrapped, &inner); err != nil {
			return errorsmod.Wrap(errortypes.ErrInvalidRequest, "cannot unpack authz exec message")
		}
		if err := walkAuthzExec(cdc, inner, depth+1, visit); err != nil {
			return err
		}
	}
	return nil
}
