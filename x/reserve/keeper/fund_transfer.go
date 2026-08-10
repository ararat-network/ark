package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/reserve/types"
)

// transferToFund moves Reserve NOAH to another protocol fund on governance's
// authority. The destination is chosen by the calling handler and never by the
// request. It reads the live Reserve balance and nothing else — so it succeeds
// while valuation is stale or incomplete — and it opens no position and
// touches no allowance.
func (k *Keeper) transferToFund(
	ctx context.Context,
	authority string,
	amount sdk.Coin,
	minimumReserveBalance sdk.Coin,
	recipientModule string,
) error {
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), k.authority, authority); err != nil {
		return err
	}
	if err := chain.ValidateNoahCoin("transfer amount", amount); err != nil {
		return err
	}
	if !amount.IsPositive() {
		return errors.New("transfer amount must be positive")
	}
	if err := chain.ValidateNoahCoin("minimum reserve balance", minimumReserveBalance); err != nil {
		return err
	}

	reserveBefore := k.balance(ctx)
	reserveAfter, err := reserveBefore.SafeSub(amount.Amount)
	if err != nil || reserveAfter.LT(minimumReserveBalance.Amount) {
		return fmt.Errorf(
			"reserve balance %s cannot fund %s while retaining %s",
			reserveBefore,
			amount,
			minimumReserveBalance,
		)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToModule(
		ctx,
		types.StrategicReserveName,
		recipientModule,
		sdk.NewCoins(amount),
	); err != nil {
		return fmt.Errorf("transferring reserve to %s: %w", recipientModule, err)
	}
	return nil
}

// committeeTransferToFund moves Reserve NOAH to an underfunded protocol fund
// on the committee's own authority, and reports the shortfall left behind. The
// destination and the bound that governs it are chosen together by the calling
// handler and never by the request. Two bounds apply: the destination's
// shortfall, which exhausts itself exactly at the target line, and the
// mandate's NOAH floor, the same floor a deployment honours.
func (k *Keeper) committeeTransferToFund(
	ctx context.Context,
	committee string,
	expectedTerm uint64,
	amount sdk.Coin,
	recipientModule string,
	fundShortfall func(types.TreasuryCapitalReader, context.Context) (math.Int, error),
) (math.Int, error) {
	reserveMandate, err := k.authoriseCommittee(ctx, committee, expectedTerm)
	if err != nil {
		return math.Int{}, err
	}
	if err := chain.ValidateNoahCoin("transfer amount", amount); err != nil {
		return math.Int{}, err
	}
	if !amount.IsPositive() {
		return math.Int{}, errors.New("transfer amount must be positive")
	}
	if k.treasuryReader == nil {
		return math.Int{}, errors.New("reserve capital requirement reader is not wired")
	}
	gap, err := fundShortfall(k.treasuryReader, ctx)
	if err != nil {
		return math.Int{}, err
	}
	if amount.Amount.GT(gap) {
		return math.Int{}, fmt.Errorf(
			"transfer %s exceeds the %s shortfall %s",
			amount,
			recipientModule,
			chain.NoahCoin(gap),
		)
	}

	balance := k.balance(ctx)
	remaining, err := balance.SafeSub(amount.Amount)
	if err != nil || remaining.LT(reserveMandate.MinimumNoahBalance.Amount) {
		return math.Int{}, fmt.Errorf(
			"reserve balance %s cannot fund %s while retaining the mandate floor %s",
			balance,
			amount,
			reserveMandate.MinimumNoahBalance,
		)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToModule(
		ctx,
		types.StrategicReserveName,
		recipientModule,
		sdk.NewCoins(amount),
	); err != nil {
		return math.Int{}, fmt.Errorf("transferring reserve to %s: %w", recipientModule, err)
	}
	return gap.Sub(amount.Amount), nil
}
