package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	arkmetrics "ark/pkg/metrics"
	"ark/x/oracle/types"
)

// EndBlocker settles periodic oracle rewards and slashing.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.EndBlock)()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	accounting, err := k.Accounting.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting accounting state: %w", err)
	}
	accountingChanged := false

	if chain.IsPeriodLastBlockFrom(ctx, accounting.RewardWindowStartHeight, accounting.RewardWindow) {
		if err := k.SettleRewards(ctx, accounting.RewardWindow, accounting.RewardDistributionWindow); err != nil {
			return err
		}

		// Clear score weights after reward settlement.
		if err := k.ScoreWeight.Clear(ctx, nil); err != nil {
			return fmt.Errorf("clearing score weights: %w", err)
		}

		if accounting.RewardWindow != params.RewardWindow ||
			accounting.RewardDistributionWindow != params.RewardDistributionWindow {
			accounting.RewardWindow = params.RewardWindow
			accounting.RewardDistributionWindow = params.RewardDistributionWindow
			accounting.RewardWindowStartHeight = uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()) + 1
			accountingChanged = true
		}
	}

	if chain.IsPeriodLastBlockFrom(ctx, accounting.SlashWindowStartHeight, accounting.SlashWindow) {
		if err := k.SettleSlash(ctx, accounting.SlashWindow); err != nil {
			return err
		}

		// Clear miss counts after slash settlement.
		if err := k.MissCount.Clear(ctx, nil); err != nil {
			return fmt.Errorf("clearing miss counts: %w", err)
		}

		if accounting.SlashWindow != params.SlashWindow {
			accounting.SlashWindow = params.SlashWindow
			accounting.SlashWindowStartHeight = uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()) + 1
			accountingChanged = true
		}
	}

	if accountingChanged {
		if err := k.Accounting.Set(ctx, accounting); err != nil {
			return fmt.Errorf("setting accounting state: %w", err)
		}
	}

	return nil
}
