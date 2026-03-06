package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SlashAndResetMissCounters do slash any operator who over criteria & clear all operators miss counter to zero
func (k Keeper) SlashAndResetMissCounters(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	distributionHeight := height - sdk.ValidatorUpdateDelay - 1

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	// slash_window / vote_period
	votePeriodsPerWindow := uint64(
		math.LegacyNewDec(int64(params.SlashWindow)).
			QuoInt64(int64(params.VotePeriod)).
			TruncateInt64(),
	)
	powerReduction := k.stakingKeeper.PowerReduction(ctx)

	if err := k.MissCounter.Walk(ctx, nil, func(operator sdk.ValAddress, missCounter uint64) (bool, error) {
		// Calculate valid vote rate; (SlashWindow - MissCounter)/SlashWindow
		validVoteRate := math.LegacyNewDec(int64(votePeriodsPerWindow - missCounter)).
			QuoInt64(int64(votePeriodsPerWindow))

		// Penalize the validator whose the valid vote rate is smaller than min threshold
		if validVoteRate.LT(params.MinValidPerWindow) {
			validator := k.stakingKeeper.Validator(ctx, operator)
			if validator.IsBonded() && !validator.IsJailed() {
				consAddr, err := validator.GetConsAddr()
				if err != nil {
					return true, err
				}

				k.stakingKeeper.Slash(
					ctx, consAddr,
					distributionHeight, validator.GetConsensusPower(powerReduction), params.SlashFraction,
				)
				k.stakingKeeper.Jail(ctx, consAddr)
			}
		}

		if err := k.MissCounter.Remove(ctx, operator); err != nil {
			return true, fmt.Errorf("removing miss counter: %w", err)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating miss counter: %w", err)
	}

	return nil
}
