package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	arkmetrics "ark/pkg/metrics"
	"ark/x/treasury/types"
)

// BeginBlocker refreshes derived tax caps when required and advances the
// reward-funding window before Distribution consumes the previous block's fees.
//
// Only the cap refresh reads the membership list. Reward funding asks the
// registry for pricing verdicts instead, so it never sees a membership
// snapshot it would have to keep honest.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.BeginBlock)()

	if err := k.refreshTaxCaps(ctx); err != nil {
		return err
	}

	if sdk.UnwrapSDKContext(ctx).BlockHeight() > 1 {
		funding, err := k.updateRewardFunding(ctx)
		if err != nil {
			return err
		}
		if funding.BlocksRemaining > 0 {
			return nil
		}
		if err := k.settleRewardFunding(ctx, funding); err != nil {
			return err
		}
		if err := k.RewardFunding.Set(ctx, types.DefaultRewardFundingState()); err != nil {
			return fmt.Errorf("resetting reward funding state: %w", err)
		}
	}

	return nil
}
