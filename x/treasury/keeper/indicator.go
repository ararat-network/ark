package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "noah/pkg/chain"
	"noah/x/treasury/types"
)

// GetEpoch returns current epoch (current block height + cumulated block height of past chains)
func (k Keeper) GetEpoch(ctx context.Context) uint64 {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return uint64(sdkCtx.BlockHeight()) / chain.BlocksPerWeek
}

// Computes important economic indicators for the stability of Noah currencies.
// Mining Rewards = Fees + Seigniorage for a given epoch

// UpdateIndicators updates internal indicators
func (k Keeper) UpdateIndicators(ctx context.Context) error {
	epoch := k.GetEpoch(ctx)
	totalStakedArk, err := k.stakingKeeper.TotalValidatorPower(ctx)
	if err != nil {
		return fmt.Errorf("getting total staked ark: %w", err)
	}
	epochTaxProceeds, err := k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting tax proceeds: %w", err)
	}
	taxProceeds := sdk.NewDecCoinsFromCoins(epochTaxProceeds.TaxProceeds...)
	taxRewards := k.alignCoins(ctx, taxProceeds, chain.MicroSDRDenom)

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
	seigniorageRewards := sdk.DecCoins{sdk.NewDecCoinFromDec(chain.MicroArkDenom, seigniorageRewardsAmt)}
	seigniorageReward := k.alignCoins(ctx, seigniorageRewards, chain.MicroSDRDenom)

	epochState := types.EpochState{
		Epoch:             epoch,
		TaxReward:         taxRewards,
		SeigniorageReward: seigniorageReward,
		TotalStakedArk:    totalStakedArk,
	}
	if err := k.EpochStates.Set(ctx, epoch, epochState); err != nil {
		return fmt.Errorf("setting epoch state: %w", err)
	}

	return nil
}

// alignCoins aligns the coins to the given denom through the market swap.
// Failed conversions are logged and skipped so indicator updates remain best-effort.
func (k Keeper) alignCoins(ctx context.Context, coins sdk.DecCoins, denom string) (alignedAmt math.LegacyDec) {
	alignedAmt = math.LegacyZeroDec()
	for _, coinReward := range coins {
		if coinReward.Denom != denom {
			swappedReward, err := k.marketKeeper.ComputeOracleRate(ctx, coinReward, denom)
			if err != nil {
				k.Logger(ctx).Warn(
					"skipping treasury indicator coin alignment",
					"source_denom", coinReward.Denom,
					"source_amount", coinReward.Amount.String(),
					"target_denom", denom,
					"error", err,
				)
				continue
			}
			alignedAmt = alignedAmt.Add(swappedReward.Amount)
		} else {
			alignedAmt = alignedAmt.Add(coinReward.Amount)
		}
	}

	return alignedAmt
}
