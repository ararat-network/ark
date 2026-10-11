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

// getBook reads an account's reservations; empty before its first grant.
func (k *Keeper) getBook(ctx context.Context, account string) (types.AccountBook, error) {
	b, err := k.Books.Get(ctx, account)
	if errors.Is(err, collections.ErrNotFound) {
		return types.EmptyBook(account), nil
	}
	return b, err
}

// uncommitted reads an account's book and Bank balance for AccountBook.Uncommitted.
func (k *Keeper) uncommitted(ctx context.Context, account, denom string) (math.Int, error) {
	b, err := k.getBook(ctx, account)
	if err != nil {
		return math.Int{}, err
	}
	return b.Uncommitted(k.bankKeeper.GetBalance(ctx, k.accountKeeper.GetModuleAddress(account), denom))
}

// draw commits amount from an account and reserves it in the account's book: from the member
// tranche, or from the contributor account's uncommitted custody in the denomination. The coins stay
// where they are.
func (k *Keeper) draw(ctx context.Context, account string, amount sdk.Coin) error {
	b, err := k.getBook(ctx, account)
	if err != nil {
		return err
	}
	if account == types.MembersName {
		tranche, err := k.MemberTranche.Get(ctx)
		if err != nil {
			return err
		}
		if tranche.LT(amount.Amount) {
			return fmt.Errorf("%s exceeds the open member tranche", amount)
		}
		if err := k.MemberTranche.Set(ctx, tranche.Sub(amount.Amount)); err != nil {
			return err
		}
	} else {
		available, err := b.Uncommitted(k.bankKeeper.GetBalance(ctx, k.accountKeeper.GetModuleAddress(account), amount.Denom))
		if err != nil {
			return err
		}
		if available.LT(amount.Amount) {
			return errors.New("insufficient uncommitted disbursement funds")
		}
	}
	if err := b.Reserve(amount); err != nil {
		return err
	}
	return k.Books.Set(ctx, account, b)
}
