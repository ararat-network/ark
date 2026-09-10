package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/market/types"
)

// AuthoriseCommittee checks exact signer, term, and active window for handlers and priority
// admission. Callers enforce the policy corridor or Tobin action bounds.
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
