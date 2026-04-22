package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SlashAndResetMissCounts slashes validators who missed too many votes and resets all miss counters
func (k Keeper) SlashAndResetMissCounts(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	distributionHeight := height - sdk.ValidatorUpdateDelay - 1

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	// calculate votePeriodsPerWindow = slash_window / vote_period
	votePeriodsPerWindow := math.LegacyNewDec(int64(params.SlashWindow)).
		Quo(math.LegacyNewDec(int64(params.VotePeriod)))
	powerReduction := k.stakingKeeper.PowerReduction(ctx)

	if err := k.MissCount.Walk(ctx, nil, func(valAddr sdk.ValAddress, missCount uint64) (bool, error) {
		// calculate valid vote rate; (votePeriodsPerWindow - missCount) / votePeriodsPerWindow
		validVoteRate := votePeriodsPerWindow.
			Sub(math.LegacyNewDec(int64(missCount))).
			Quo(votePeriodsPerWindow)

		// slash and jail validators who voted less than the minimum required rate
		if validVoteRate.LT(params.MinValidPerWindow) {
			validator := k.stakingKeeper.Validator(ctx, valAddr)
			if validator != nil && validator.IsBonded() && !validator.IsJailed() {
				consAddr, err := validator.GetConsAddr()
				if err != nil {
					k.Logger(ctx).Warn("failed to get consensus address", "validator", validator, "error", err)
				} else {
					k.stakingKeeper.Slash(
						ctx, consAddr,
						distributionHeight, validator.GetConsensusPower(powerReduction), params.SlashFraction,
					)
					k.stakingKeeper.Jail(ctx, consAddr)
				}
			}
		}

		if err := k.MissCount.Remove(ctx, valAddr); err != nil {
			return true, fmt.Errorf("removing miss counter: %w", err)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating miss counter: %w", err)
	}

	return nil
}
