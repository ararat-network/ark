package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/treasury/types"
)

// AuthoriseCommittee checks the signer against the live economic mandate,
// returning it for the caller's own constraints: the exact signer, the exact
// term, and the active window. Every committee handler runs it first, and the
// priority lane vouches through it at CheckTx, so the lane refuses exactly
// what the handlers refuse.
func (k Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.EconomicMandate, error) {
	economicMandate, err := k.EconomicMandate.Get(ctx)
	if err != nil {
		return types.EconomicMandate{}, fmt.Errorf("getting economic mandate: %w", err)
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if err := economicMandate.Authorise(committee, expectedTerm, height); err != nil {
		return types.EconomicMandate{}, fmt.Errorf("%s: %w", types.EconomicMandateLabel, err)
	}
	return economicMandate, nil
}
