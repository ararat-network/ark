package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	servicemetrics "noah/service/metrics"
	core "noah/types"
	"noah/x/oracle/types"
)

// EndBlocker settles periodic oracle rewards and slashing.
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer servicemetrics.RecordABCIMethodLatency(ctx, types.ModuleName, servicemetrics.EndBlock)()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	if core.IsPeriodLastBlock(ctx, params.RewardWindow) {
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

	if core.IsPeriodLastBlock(ctx, params.SlashWindow) {
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
