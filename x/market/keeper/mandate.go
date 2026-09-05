package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/market/types"
)

// AuthoriseCommittee checks the signer against the live conversion mandate,
// returning it for the caller's own constraints: the exact signer, the exact
// term, and the active window. Every committee handler runs it first, and the
// priority lane vouches through it at CheckTx, so the lane refuses exactly
// what the handlers refuse.
func (k Keeper) AuthoriseCommittee(ctx context.Context, committee string, expectedTerm uint64) (types.ConversionMandate, error) {
	conversionMandate, err := k.ConversionMandate.Get(ctx)
	if err != nil {
		return types.ConversionMandate{}, fmt.Errorf("getting conversion mandate: %w", err)
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if err := conversionMandate.Authorise(committee, expectedTerm, height); err != nil {
		return types.ConversionMandate{}, fmt.Errorf("%s: %w", types.ConversionMandateLabel, err)
	}
	return conversionMandate, nil
}
