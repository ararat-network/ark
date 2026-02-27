package keeper

import (
	"context"
	"fmt"

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
	whitelist := k.oracleKeeper.Whitelist(ctx)

	var newCaps sdk.Coins
	for _, denom := range whitelist {
		// keep sdr tax cap
		if denom.Name == taxPolicyCap.Denom {
			continue
		}

		newDecCap, err := k.marketKeeper.ComputeOracleRate(ctx, taxPolicyCap, denom.Name)
		if err == nil {
			newCap, _ := newDecCap.TruncateDecimal()
			newCaps = append(newCaps, newCap)
			if err := k.TaxCaps.Set(ctx, newCap.Denom, newCap.Amount); err != nil {
				return nil, fmt.Errorf("setting tax cap: %w", err)
			}
		}
	}

	return newCaps, nil
}

// UpdateTaxPolicy updates tax-rate with t(t+1) = t(t) * (TL_year(t) + INC) / TL_month(t)
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
	tlYear, err := k.rollingAverageIndicator(ctx, params.WindowLong)
	if err != nil {
		return math.LegacyZeroDec(), err
	}
	tlMonth, err := k.rollingAverageIndicator(ctx, params.WindowShort)
	if err != nil {
		return math.LegacyZeroDec(), err
	}

	var newTaxRate math.LegacyDec
	// No revenues, hike as much as possible.
	if tlMonth.Equal(math.LegacyZeroDec()) {
		newTaxRate = params.TaxPolicy.RateMax
	} else {
		newTaxRate = oldTaxRate.Mul(tlYear.Mul(inc)).Quo(tlMonth)
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

	newRewardWeight = params.RewardPolicy.Clamp(oldWeight, newRewardWeight)

	// Set the new reward weight
	if err := k.RewardWeight.Set(ctx, newRewardWeight); err != nil {
		return math.LegacyZeroDec(), fmt.Errorf("setting reward weight: %w", err)
	}
	return newRewardWeight, nil
}
