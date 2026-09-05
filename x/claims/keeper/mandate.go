package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/claims/types"
)

// AuthoriseCommittee checks the signer against the live mandate, returning it
// for the caller's own constraints: the exact signer, the exact term, and the
// active window. Every committee handler runs it first, and the priority lane
// vouches through it at CheckTx, so the lane refuses exactly what the
// handlers refuse.
func (k Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.ClaimsMandate, error) {
	claimsMandate, err := k.ClaimsMandate.Get(ctx)
	if err != nil {
		return types.ClaimsMandate{}, fmt.Errorf("getting Claims mandate: %w", err)
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if err := claimsMandate.Authorise(committee, expectedTerm, height); err != nil {
		return types.ClaimsMandate{}, fmt.Errorf("%s: %w", types.ClaimsMandateLabel, err)
	}
	return claimsMandate, nil
}
