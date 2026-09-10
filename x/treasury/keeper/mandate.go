package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/treasury/types"
)

// AuthoriseCommittee validates exact signer, term, and active window for economic handlers and
// priority-lane admission. Callers enforce delegated policy bounds.
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
