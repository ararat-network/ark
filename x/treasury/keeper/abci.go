package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	arkmetrics "ark/pkg/metrics"
	"ark/x/treasury/types"
)

// BeginBlocker refreshes derived tax caps when required and advances the
// reward-funding window before Distribution consumes the previous block's fees.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.BeginBlock)()

	mismatch, err := k.TaxCapDenomsMismatch(ctx)
	if err != nil {
		return fmt.Errorf("checking tax cap denominations: %w", err)
	}
	if mismatch || chain.IsPeriodLastBlock(ctx, chain.BlocksPerWeek) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return fmt.Errorf("getting params: %w", err)
		}
		caps, err := k.BuildTaxCaps(ctx, params)
		if err != nil {
			if isValuationUnavailable(err) {
				reason := eventSkipReason(err)
				k.Logger(ctx).Warn("skipping Treasury tax-cap refresh", "reason", reason, "error", err)
				if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTaxCapsUpdateSkipped{
					Reason: reason,
				}); err != nil {
					return fmt.Errorf("emitting Treasury tax-cap refresh skip event: %w", err)
				}
			} else {
				return fmt.Errorf("building tax caps: %w", err)
			}
		} else {
			if err := k.ReplaceTaxCaps(ctx, caps); err != nil {
				return fmt.Errorf("replacing tax caps: %w", err)
			}
			if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTaxCapsUpdated{
				TaxCaps: caps,
			}); err != nil {
				return fmt.Errorf("emitting Treasury tax-cap update event: %w", err)
			}
		}
	}

	if sdk.UnwrapSDKContext(ctx).BlockHeight() > 1 {
		funding, err := k.UpdateRewardFunding(ctx)
		if err != nil {
			return err
		}
		if funding.BlocksRemaining > 0 {
			return nil
		}
		if err := k.SettleRewardFunding(ctx, funding); err != nil {
			return err
		}
		if err := k.RewardFunding.Set(ctx, types.DefaultRewardFundingState()); err != nil {
			return fmt.Errorf("resetting reward funding state: %w", err)
		}
	}

	return nil
}
