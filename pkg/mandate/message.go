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

// Vouch adapts the module's AuthoriseCommittee check for committee admission.
// A refusal rejects the transaction: committee actions cannot establish their
// own appointment prerequisites. Storage and authorisation errors propagate.
func Vouch[M any](authorise func(context.Context, string, uint64) (M, error)) func(sdk.Context, sdk.Msg) (bool, error) {
	return func(ctx sdk.Context, msg sdk.Msg) (bool, error) {
		committee, ok := msg.(Message)
		if !ok {
			return false, errorsmod.Wrapf(errortypes.ErrInvalidRequest, "%s carries no committee and term", sdk.MsgTypeURL(msg))
		}
		if _, err := authorise(ctx, committee.GetCommittee(), committee.GetExpectedTerm()); err != nil {
			return false, err
		}
		return true, nil
	}
}
