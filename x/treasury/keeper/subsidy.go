package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
)

// SubsidyPoolBalance is the subsidy pool's NOAH balance.
func (k Keeper) SubsidyPoolBalance(ctx context.Context) math.Int {
	return k.getBalance(ctx, types.SubsidyPoolName)
}

// ReturnSubsidy moves subsidy NOAH into the community pool under governance
// authority. The minimum is the proposal's stale-state guard: the return
// fails if it would leave less. Nothing else bounds it. A floor read from the
// live targets would be a projection governance can move under, and an
// over-return leaves only a shortfall the next window reports and a deposit
// reverses.
func (k Keeper) ReturnSubsidy(ctx context.Context, authority string, amount, minimumBalance sdk.Coin) error {
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), k.authority, authority); err != nil {
		return err
	}
	if err := chain.ValidateNoahCoin("return amount", amount); err != nil {
		return err
	}
	if !amount.IsPositive() {
		return errors.New("return amount must be positive")
	}
	if err := chain.ValidateNoahCoin("minimum subsidy balance", minimumBalance); err != nil {
		return err
	}

	before := k.getBalance(ctx, types.SubsidyPoolName)
	remaining, err := before.SafeSub(amount.Amount)
	if err != nil || remaining.LT(minimumBalance.Amount) {
		return fmt.Errorf(
			"subsidy pool balance %s cannot return %s while retaining %s",
			before,
			amount,
			minimumBalance,
		)
	}

	if err := k.distributionKeeper.FundCommunityPool(
		ctx,
		sdk.NewCoins(amount),
		k.accountKeeper.GetModuleAddress(types.SubsidyPoolName),
	); err != nil {
		return fmt.Errorf("funding the community pool: %w", err)
	}

	return sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventSubsidyReturned{
		Denom:     chain.NoahBaseDenom,
		Amount:    amount.Amount,
		Remaining: remaining,
	})
}
