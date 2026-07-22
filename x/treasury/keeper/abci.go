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
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.BeginBlock)()

	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return fmt.Errorf("checking tax cap denominations: %w", err)
	}
	configuredDenoms := make(map[string]struct{}, len(tobinTaxes))
	for _, tobinTax := range tobinTaxes {
		configuredDenoms[tobinTax.Denom] = struct{}{}
	}
	if err := k.refreshTaxCaps(ctx, tobinTaxes, configuredDenoms); err != nil {
		return err
	}

	if sdk.UnwrapSDKContext(ctx).BlockHeight() > 1 {
		funding, err := k.updateRewardFunding(ctx, configuredDenoms)
		if err != nil {
			return err
		}
		if funding.BlocksRemaining > 0 {
			return nil
		}
		if err := k.settleRewardFunding(ctx, funding, configuredDenoms); err != nil {
			return err
		}
		if err := k.RewardFunding.Set(ctx, types.DefaultRewardFundingState()); err != nil {
			return fmt.Errorf("resetting reward funding state: %w", err)
		}
	}

	return nil
}
