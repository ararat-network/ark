package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "noah/pkg/chain"
	noahmetrics "noah/pkg/metrics"
	"noah/x/oracle/types"
)

// EndBlocker settles periodic oracle rewards and slashing.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer noahmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, noahmetrics.EndBlock)()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	if chain.IsPeriodLastBlock(ctx, params.RewardWindow) {
		if err := k.SettleRewards(ctx, params.RewardWindow, params.RewardDistributionWindow); err != nil {
			return err
		}

		// Clear score weights after reward settlement.
		if err := k.ScoreWeight.Walk(ctx, nil, func(valAddr sdk.ValAddress, _ uint64) (bool, error) {
			return false, k.ScoreWeight.Remove(ctx, valAddr)
		}); err != nil {
			return fmt.Errorf("clearing score weights: %w", err)
		}
	}

	if chain.IsPeriodLastBlock(ctx, params.SlashWindow) {
		if err := k.SettleSlash(ctx); err != nil {
			return err
		}

		// Clear miss counts after slash settlement.
		if err := k.MissCount.Walk(ctx, nil, func(valAddr sdk.ValAddress, _ uint64) (bool, error) {
			return false, k.MissCount.Remove(ctx, valAddr)
		}); err != nil {
			return fmt.Errorf("clearing miss counts: %w", err)
		}
	}

	return nil
}
