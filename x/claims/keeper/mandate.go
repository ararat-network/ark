package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/claims/types"
)

// AuthoriseCommittee validates the exact signer, term, and active window for both committee
// handlers and priority-lane eligibility. Callers enforce action-specific constraints.
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
