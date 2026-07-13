package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// GetEpoch returns current epoch (current block height + cumulated block height of past chains)
func (k Keeper) GetEpoch(ctx context.Context) uint64 {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return uint64(sdkCtx.BlockHeight()) / chain.BlocksPerWeek
}

// Computes important economic indicators for the stability of Ark currencies.
// Mining Rewards = Fees + Seigniorage for a given epoch

// UpdateIndicators updates internal indicators
func (k Keeper) UpdateIndicators(ctx context.Context, rates oracletypes.RateSnapshot) error {
	epoch := k.GetEpoch(ctx)
	totalStakedNoah, err := k.stakingKeeper.TotalValidatorPower(ctx)
	if err != nil {
		return fmt.Errorf("getting total staked noah: %w", err)
	}
	epochTaxProceeds, err := k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting tax proceeds: %w", err)
	}
	taxProceeds := sdk.NewDecCoinsFromCoins(epochTaxProceeds.TaxProceeds...)
	taxRewards, err := alignCoins(taxProceeds, chain.MicroSDRDenom, rates)
	if err != nil {
		return fmt.Errorf("aligning tax proceeds: %w", err)
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
	seigniorageRewards := sdk.DecCoins{sdk.NewDecCoinFromDec(chain.MicroNoahDenom, seigniorageRewardsAmt)}
	seigniorageReward, err := alignCoins(seigniorageRewards, chain.MicroSDRDenom, rates)
	if err != nil {
		return fmt.Errorf("aligning seigniorage rewards: %w", err)
	}

	epochState := types.EpochState{
		Epoch:             epoch,
		TaxReward:         taxRewards,
		SeigniorageReward: seigniorageReward,
		TotalStakedNoah:   totalStakedNoah,
	}
	if err := k.EpochStates.Set(ctx, epoch, epochState); err != nil {
		return fmt.Errorf("setting epoch state: %w", err)
	}
	if err := k.EpochTaxProceeds.Set(ctx, types.EpochTaxProceeds{}); err != nil {
		return fmt.Errorf("resetting tax proceeds: %w", err)
	}

	return nil
}

// alignCoins aligns coins to the given denom using oracle exchange rates.
func alignCoins(coins sdk.DecCoins, denom string, rates oracletypes.RateSnapshot) (math.LegacyDec, error) {
	alignedAmt := math.LegacyZeroDec()
	for _, coinReward := range coins {
		if coinReward.Amount.IsZero() {
			continue
		}
		if coinReward.Denom != denom {
			swappedReward, err := rates.Convert(coinReward, denom)
			if err != nil {
				return math.LegacyZeroDec(), fmt.Errorf("converting %s to %s: %w", coinReward, denom, err)
			}
			alignedAmt = alignedAmt.Add(swappedReward.Amount)
		} else {
			alignedAmt = alignedAmt.Add(coinReward.Amount)
		}
	}

	return alignedAmt, nil
}
