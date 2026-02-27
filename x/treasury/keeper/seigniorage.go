package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

// SettleSeigniorage computes seigniorage and distributes it to oracle and distribution(community-pool) account
func (k Keeper) SettleSeigniorage(ctx context.Context) error {
	// Mint seigniorage for oracle and community pool
	seigniorageArkAmt, err := k.ComputeEpochSeigniorage(ctx)
	if err != nil {
		return fmt.Errorf("computing epoch seiniorage:%w", err)
	}
	if seigniorageArkAmt.LTE(math.ZeroInt()) {
		return nil
	}

	// Settle current epoch seigniorage
	rewardWeight, err := k.RewardWeight.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting reward weight: %w", err)
	}

	// Align seigniorage to usdr
	seigniorageDecCoin := sdk.NewDecCoin(core.MicroArkDenom, seigniorageArkAmt)

	// Mint seigniorage
	seigniorageCoin, _ := seigniorageDecCoin.TruncateDecimal()
	seigniorageCoins := sdk.NewCoins(seigniorageCoin)
	if seigniorageCoins.IsValid() {
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, seigniorageCoins); err != nil {
			return err
		}
	}
	seigniorageAmt := seigniorageCoin.Amount

	// Send reward to oracle module
	oracleRewardAmt := rewardWeight.MulInt(seigniorageAmt).TruncateInt()
	oracleRewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, oracleRewardAmt))
	err = k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.oracleModuleName, oracleRewardCoins)
	if err != nil {
		return err
	}

	// Send left to distribution module
	leftAmt := seigniorageAmt.Sub(oracleRewardAmt)
	leftCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, leftAmt))
	if leftCoins.IsValid() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.ModuleName,
			k.distributionModuleName,
			leftCoins,
		); err != nil {
			return err
		}

		// Update distribution community pool
		feePool := k.distrKeeper.GetFeePool(ctx)
		feePool.CommunityPool = feePool.CommunityPool.Add(sdk.NewDecCoinsFromCoins(leftCoins...)...)
		k.distrKeeper.SetFeePool(ctx, feePool)
	}
	return nil
}
