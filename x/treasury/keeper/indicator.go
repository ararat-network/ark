package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

// GetEpoch returns current epoch of (current block height + cumulated block height of past chains)
func (k Keeper) GetEpoch(ctx context.Context) uint64 {
	sdkctx := sdk.UnwrapSDKContext(ctx)
	return uint64(sdkctx.BlockHeight()) / core.BlocksPerWeek
}

// Computes important economic indicators for the stability of Noah currencies.
// Mining Rewards = Fees + Seigniorage for a given epoch

// alignCoins align the coins to the given denom through the market swap
func (k Keeper) alignCoins(ctx context.Context, coins sdk.DecCoins, denom string) (alignedAmt math.LegacyDec) {
	alignedAmt = math.LegacyZeroDec()
	for _, coinReward := range coins {
		if coinReward.Denom != denom {
			swappedReward, err := k.marketKeeper.ComputeOracleRate(ctx, coinReward, denom)
			if err != nil {
				continue
			}
			alignedAmt = alignedAmt.Add(swappedReward.Amount)
		} else {
			alignedAmt = alignedAmt.Add(coinReward.Amount)
		}
	}

	return alignedAmt
}

// UpdateIndicators updates internal indicators
func (k Keeper) UpdateIndicators(ctx context.Context) error {
	epoch := k.GetEpoch(ctx)

	// Compute Total Staked Ark
	totalStakedArk := k.stakingKeeper.TotalBondedTokens(ctx)

	// Compute Tax Rewards
	taxProceeds, err := k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting tax proceeds: %w", err)
	}
	taxRewards := sdk.NewDecCoinsFromCoins(taxProceeds.TaxProceeds...)
	taxReward := k.alignCoins(ctx, taxRewards, core.MicroSDRDenom)

	// Reset tax proceeds after computing TotalStakedArk for the next epoch
	if err := k.EpochTaxProceeds.Set(ctx, types.EpochTaxProceeds{}); err != nil {
		return fmt.Errorf("resetting tax proceeds: %w", err)
	}

	// Compute Seigniorage Rewards
	seigniorage, err := k.ComputeEpochSeigniorage(ctx)
	if err != nil {
		return fmt.Errorf("computing seigniorage: %w", err)
	}
	rewardWeight, err := k.RewardWeight.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting reward weight: %w", err)
	}
	seigniorageRewardsAmt := rewardWeight.MulInt(seigniorage)
	seigniorageRewards := sdk.DecCoins{sdk.NewDecCoinFromDec(core.MicroArkDenom, seigniorageRewardsAmt)}
	seigniorageReward := k.alignCoins(ctx, seigniorageRewards, core.MicroSDRDenom)

	epochState := types.EpochState{
		Epoch:             epoch,
		TaxReward:         taxReward,
		SeigniorageReward: seigniorageReward,
		TotalStakedArk:    totalStakedArk,
	}
	if err := k.EpochStates.Set(ctx, epoch, epochState); err != nil {
		return fmt.Errorf("setting epoch state: %w", err)
	}

	return nil
}

// sumIndicator returns the sum of the indicator over several epochs.
// If current epoch < epochs, we return the best we can and return sumIndicator(currentEpoch).
// Missing epoch states are skipped gracefully (treated as zero contribution).
// Return values are (taxRewardSum, seigniorageRewardSum, error)
func (k Keeper) sumIndicator(ctx context.Context, epochs uint64) (math.LegacyDec, math.LegacyDec, error) {
	taxRewardSum := math.LegacyZeroDec()
	seigniorageRewardSum := math.LegacyZeroDec()
	curEpoch := k.GetEpoch(ctx)

	n := min(epochs, curEpoch+1)
	for j := uint64(0); j < n; j++ {
		val, err := k.EpochStates.Get(ctx, curEpoch-j)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return math.LegacyZeroDec(), math.LegacyZeroDec(), fmt.Errorf("getting epoch state: %w", err)
		}
		taxRewardSum = taxRewardSum.Add(val.TaxReward)
		seigniorageRewardSum = seigniorageRewardSum.Add(val.SeigniorageReward)
	}

	return taxRewardSum, seigniorageRewardSum, nil
}

// rollingAverageIndicator returns the rolling average of the indicator over several epochs.
// If current epoch < epochs, we return the best we can and return rollingAverageIndicator(currentEpoch).
// Missing epoch states are skipped and excluded from the denominator, so the average
// reflects only epochs with actual data rather than being diluted by gaps.
func (k Keeper) rollingAverageIndicator(ctx context.Context, epochs uint64) (math.LegacyDec, error) {
	sum := math.LegacyZeroDec()
	curEpoch := k.GetEpoch(ctx)

	n := min(epochs, curEpoch+1)
	var counted uint64
	for j := uint64(0); j < n; j++ {
		val, err := k.EpochStates.Get(ctx, curEpoch-j)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return math.LegacyZeroDec(), fmt.Errorf("getting epoch state: %w", err)
		}
		counted++
		if val.TaxReward.IsZero() || val.TotalStakedArk.IsZero() {
			continue
		}
		sum = sum.Add(val.TaxReward.QuoInt(val.TotalStakedArk))
	}

	if counted == 0 {
		return sum, nil
	}

	return sum.QuoInt64(int64(counted)), nil
}
