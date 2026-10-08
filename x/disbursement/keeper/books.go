package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/disbursement/types"
)

// book is an account's reservations; empty before its first grant.
func (k *Keeper) book(ctx context.Context, account string) (types.AccountBook, error) {
	b, err := k.Books.Get(ctx, account)
	if errors.Is(err, collections.ErrNotFound) {
		return types.EmptyBook(account), nil
	}
	return b, err
}

// uncommitted reads an account's book and Bank balance for AccountBook.Uncommitted.
func (k *Keeper) uncommitted(ctx context.Context, account, denom string) (math.Int, error) {
	b, err := k.book(ctx, account)
	if err != nil {
		return math.Int{}, err
	}
	return b.Uncommitted(k.bankKeeper.GetBalance(ctx, k.accountKeeper.GetModuleAddress(account), denom))
}

// draw commits amount from an account: the member tranche, or the contributor account's uncommitted
// custody in the denomination. The coins stay where they are; the caller reserves them.
func (k *Keeper) draw(ctx context.Context, account string, amount sdk.Coin) error {
	if account != types.MembersName {
		available, err := k.uncommitted(ctx, account, amount.Denom)
		if err != nil {
			return err
		}
		if available.LT(amount.Amount) {
			return errors.New("insufficient uncommitted disbursement funds")
		}
		return nil
	}
	tranche, err := k.MemberTranche.Get(ctx)
	if err != nil {
		return err
	}
	if tranche.LT(amount.Amount) {
		return fmt.Errorf("%s exceeds the open member tranche", amount)
	}
	return k.MemberTranche.Set(ctx, tranche.Sub(amount.Amount))
}
