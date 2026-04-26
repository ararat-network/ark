package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UpdateTaxCap updates all denom's tax cap
func (k Keeper) UpdateTaxCap(ctx context.Context) (sdk.Coins, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	taxPolicyCap := sdk.NewDecCoinFromCoin(params.TaxPolicy.Cap)
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return nil, err
	}

	var newCaps sdk.Coins
	for _, denom := range tobinTaxes {
		// keep sdr tax cap
		if denom.Denom == taxPolicyCap.Denom {
			continue
		}

		newDecCap, err := k.marketKeeper.ComputeOracleRate(ctx, taxPolicyCap, denom.Denom)
		if err != nil {
			k.Logger(ctx).Warn(
				"skipping tax cap update",
				"denom", denom.Denom,
				"cap_denom", taxPolicyCap.Denom,
				"cap_amount", taxPolicyCap.Amount.String(),
				"err", err,
			)
			continue
		}

		newCap, _ := newDecCap.TruncateDecimal()
		newCaps = append(newCaps, newCap)
		if err := k.TaxCaps.Set(ctx, newCap.Denom, newCap.Amount); err != nil {
			return nil, fmt.Errorf("setting tax cap: %w", err)
		}
	}

	return newCaps, nil
}

// UpdateTaxPolicy updates tax-rate with t(t+1) = t(t) * (TRA_year(t) + INC) / TRA_month(t)
func (k Keeper) UpdateTaxPolicy(ctx context.Context) (math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting params: %w", err)
	}

	oldTaxRate, err := k.TaxRate.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting tax rate: %w", err)
	}
	inc := params.MiningIncrement
	traYear, err := k.rollingAverageIndicator(ctx, params.WindowLong)
	if err != nil {
		return math.LegacyZeroDec(), err
	}
	traMonth, err := k.rollingAverageIndicator(ctx, params.WindowShort)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	var newTaxRate math.LegacyDec
	// No revenues, hike as much as possible.
	if traMonth.Equal(math.LegacyZeroDec()) {
		newTaxRate = params.TaxPolicy.RateMax
	} else {
		newTaxRate = oldTaxRate.Mul(traYear.Mul(inc)).Quo(traMonth)
	}

	newTaxRate = params.TaxPolicy.Clamp(oldTaxRate, newTaxRate)

	// Set the new tax rate to the store
	if err := k.TaxRate.Set(ctx, newTaxRate); err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("setting tax rate: %w", err)
	}
	return newTaxRate, nil
}

// UpdateRewardPolicy updates reward-weight with w(t+1) = w(t)*SB_target/SB_rolling(t)
func (k Keeper) UpdateRewardPolicy(ctx context.Context) (math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting params: %w", err)
	}

	oldWeight, err := k.RewardWeight.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("getting reward weight: %w", err)
	}
	sbTarget := params.SeigniorageBurdenTarget

	taxRewardSum, seigniorageSum, err := k.sumIndicator(ctx, params.WindowShort)
	if err != nil {
		return math.LegacyZeroDec(), err
	}
	totalSum := taxRewardSum.Add(seigniorageSum)

	// No revenues; hike as much as possible
	var newRewardWeight math.LegacyDec
	if totalSum.Equal(math.LegacyZeroDec()) || seigniorageSum.Equal(math.LegacyZeroDec()) {
		newRewardWeight = params.RewardPolicy.RateMax
	} else {
		// Seigniorage burden out of total rewards
		sb := seigniorageSum.Quo(totalSum)
		newRewardWeight = oldWeight.Mul(sbTarget.Quo(sb))
	}

	// because params.Validate() ensures BurnWeight is less than 1 - RewardPolicy.RateMax we don't need a guard
	// for rewardWeight here
	newRewardWeight = params.RewardPolicy.Clamp(oldWeight, newRewardWeight)

	// Set the new reward weight
	if err := k.RewardWeight.Set(ctx, newRewardWeight); err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("setting reward weight: %w", err)
	}
	return newRewardWeight, nil
}

// sumIndicator returns the sum of the indicator over several epochs.
// If current epoch < epochs, we return the best we can and return sumIndicator(currentEpoch).
// Missing epoch states are skipped gracefully (treated as zero contribution).
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
