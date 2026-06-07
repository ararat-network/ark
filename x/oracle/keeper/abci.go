package keeper

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	if core.IsPeriodLastBlock(ctx, params.RewardWindow) {
		if err := k.SettleRewards(ctx, params.RewardWindow, params.RewardDistributionWindow); err != nil {
			return err
		}

		// reset scores for all validators
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

		// reset miss counts for all validators
		if err := k.MissCount.Walk(ctx, nil, func(valAddr sdk.ValAddress, _ uint64) (bool, error) {
			return false, k.MissCount.Remove(ctx, valAddr)
		}); err != nil {
			return fmt.Errorf("clearing miss counts: %w", err)
		}
	}

	return nil
}
