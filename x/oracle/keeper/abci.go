package keeper

import (
	"context"
	"fmt"
	"maps"
	"time"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/telemetry"

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

	if core.IsPeriodLastBlock(ctx, params.VotePeriod) {
		validatorScoreMap, err := k.BuildValidatorScoreMap(ctx)
		if err != nil {
			return err
		}

		voteTargets := make(map[string]math.LegacyDec)
		if err := k.TobinTax.Walk(ctx, nil, func(denom string, tobinTax math.LegacyDec) (bool, error) {
			voteTargets[denom] = tobinTax
			return false, nil
		}); err != nil {
			return fmt.Errorf("iterating tobin tax: %w", err)
		}
		storedTobinTaxes := maps.Clone(voteTargets)

		if err := k.UpdateExchangeRates(
			ctx,
			params.RewardBand,
			params.VoteThreshold,
			voteTargets,
			validatorScoreMap,
		); err != nil {
			return err
		}

		if err := k.CountMisses(ctx, voteTargets, validatorScoreMap); err != nil {
			return err
		}

		if err := k.RewardVoteWinners(
			ctx,
			int64(params.VotePeriod),
			int64(params.RewardDistributionWindow),
			validatorScoreMap,
		); err != nil {
			return err
		}

		if err := k.ClearVotes(ctx, params.VotePeriod); err != nil {
			return err
		}

		if err := k.SyncTobinTaxes(ctx, storedTobinTaxes, params.TobinTaxes); err != nil {
			return err
		}
	}

	// slash and reset miss counters at the end of each slash window
	if core.IsPeriodLastBlock(ctx, params.SlashWindow) {
		if err := k.SlashAndResetMissCounts(ctx); err != nil {
			return err
		}
	}

	return nil
}
