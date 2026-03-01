package keeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) (err error) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	// Check epoch last block
	if !core.IsPeriodLastBlock(ctx, core.BlocksPerWeek) {
		return nil
	}

	// Record issuance after all epoch-end work completes, so the next epoch
	// starts with an accurate snapshot. Uses errors.Join to surface deferred
	// errors without masking any earlier error from the main body.
	defer func() {
		if deferErr := k.RecordEpochInitialIssuance(ctx); deferErr != nil {
			err = errors.Join(err, fmt.Errorf("recording epoch initial issuance: %w", deferErr))
		}
	}()

	// Compute & Update internal indicators for the current epoch
	if err := k.UpdateIndicators(ctx); err != nil {
		return fmt.Errorf("updating indicators: %w", err)
	}

	// Check probation period
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if sdkCtx.BlockHeight() < int64(core.BlocksPerWeek*params.WindowProbation) {
		return nil
	}

	// Settle seigniorage to oracle & distribution(community-pool) module-account
	if err := k.SettleSeigniorage(ctx); err != nil {
		return fmt.Errorf("settling seigniorage: %w", err)
	}

	// Update tax-rate and reward-weight of next epoch
	taxRate, err := k.UpdateTaxPolicy(ctx)
	if err != nil {
		return fmt.Errorf("updating tax policy: %w", err)
	}
	rewardWeight, err := k.UpdateRewardPolicy(ctx)
	if err != nil {
		return fmt.Errorf("updating reward policy: %w", err)
	}
	taxCap, err := k.UpdateTaxCap(ctx)
	if err != nil {
		return fmt.Errorf("updating tax cap: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypePolicyUpdate,
			sdk.NewAttribute(types.AttributeKeyTaxRate, taxRate.String()),
			sdk.NewAttribute(types.AttributeKeyRewardWeight, rewardWeight.String()),
			sdk.NewAttribute(types.AttributeKeyTaxCap, taxCap.String()),
		),
	)
	return nil
}
