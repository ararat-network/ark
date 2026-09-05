package ante

import (
	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// walkAuthzExec applies visit to msg, or to each message nested inside an
// authz MsgExec. Every message policy this package enforces walks through
// here, so the recursion bound and its refusals are stated once: a bound that
// drifted between two policies would be a hole at both seams that call them —
// the ante for signed transactions, the policy router for the rest.
//
// The wrapper itself is not visited. No policy matches MsgExec, and one that
// did would want the wrapper's own signer rather than the nested message's.
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
