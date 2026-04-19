package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

// SettleSeigniorage computes seigniorage and distributes it to oracle and distribution (community-pool) account
func (k Keeper) SettleSeigniorage(ctx context.Context) error {
	// Mint seigniorage for oracle and community pool
	seigniorageArkAmt, err := k.ComputeEpochSeigniorage(ctx)
	if err != nil {
		return fmt.Errorf("computing epoch seigniorage: %w", err)
	}
	if seigniorageArkAmt.LTE(math.ZeroInt()) {
		return nil
	}
	rewardWeight, err := k.RewardWeight.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting reward weight: %w", err)
	}

	// Mint seigniorage
	seigniorageCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, seigniorageArkAmt))
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, seigniorageCoins); err != nil {
		return fmt.Errorf("minting seigniorage: %w", err)
	}

	// Send reward to oracle module
	oracleRewardAmt := rewardWeight.MulInt(seigniorageArkAmt).TruncateInt()
	if oracleRewardAmt.IsPositive() {
		oracleRewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, oracleRewardAmt))
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.ModuleName,
			k.rewardCollectorName,
			oracleRewardCoins,
		); err != nil {
			return fmt.Errorf("sending oracle reward: %w", err)
		}
	}

	// Send remaining amount to distribution module
	remainAmt := seigniorageArkAmt.Sub(oracleRewardAmt)
	if remainAmt.IsPositive() {
		leftCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, remainAmt))
		treasuryAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
		if err := k.distrKeeper.FundCommunityPool(ctx, leftCoins, treasuryAddr); err != nil {
			return fmt.Errorf("funding community pool: %w", err)
		}
	}

	return nil
}
