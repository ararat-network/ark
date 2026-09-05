package mandate

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// Message is what every committee message carries: the signer that must be
// the appointed committee, and the term it was prepared under.
type Message interface {
	sdk.Msg
	GetCommittee() string
	GetExpectedTerm() uint64
}

// Vouch adapts a module's committee authorisation, the exported check its
// handlers run first, into the admission check the priority lane runs at
// CheckTx. It refuses exactly what that check refuses and nothing more, so a
// transaction the handler would accept is never kept out of a block.
func Vouch[M any](authorise func(context.Context, string, uint64) (M, error)) func(sdk.Context, sdk.Msg) error {
	return func(ctx sdk.Context, msg sdk.Msg) error {
		committee, ok := msg.(Message)
		if !ok {
			return errorsmod.Wrapf(errortypes.ErrInvalidRequest, "%s carries no committee and term", sdk.MsgTypeURL(msg))
		}
		if _, err := authorise(ctx, committee.GetCommittee(), committee.GetExpectedTerm()); err != nil {
			return errorsmod.Wrap(errortypes.ErrUnauthorized, err.Error())
		}
		return nil
	}
}
