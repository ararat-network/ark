package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/treasury/types"
)

// BeginBlocker refreshes derived tax caps when required and advances the
// reward-funding window before Distribution consumes the previous block's fees.
func (k Keeper) BeginBlocker(ctx context.Context) error {
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
				k.Logger(ctx).Warn("skipping Treasury tax-cap refresh", "error", err)
				sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
					types.EventTypeTaxCapsUpdateSkipped,
					sdk.NewAttribute(types.AttributeKeySkipReason, err.Error()),
				))
			} else {
				return fmt.Errorf("building tax caps: %w", err)
			}
		} else {
			if err := k.ReplaceTaxCaps(ctx, caps); err != nil {
				return fmt.Errorf("replacing tax caps: %w", err)
			}
			sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeTaxCapsUpdated,
				sdk.NewAttribute(types.AttributeKeyTaxCaps, formatTaxCaps(caps)),
			))
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

func formatTaxCaps(caps []types.TaxCap) string {
	parts := make([]string, 0, len(caps))
	for _, cap := range caps {
		parts = append(parts, sdk.NewCoin(cap.Denom, cap.TaxCap).String())
	}
	return strings.Join(parts, ",")
}
