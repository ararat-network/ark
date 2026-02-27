package keeper

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	// Check epoch last block
	if !core.IsPeriodLastBlock(ctx, core.BlocksPerWeek) {
		return nil
	}

	// Update luna issuance after finish all works
	defer k.RecordEpochInitialIssuance(ctx)

	// Compute & Update internal indicators for the current epoch
	// TODO: make sure the linter catches this unhandled error and handle it
	k.UpdateIndicators(ctx)

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
	// TODO error handling
	k.SettleSeigniorage(ctx)

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
