package types

import (
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// EmptyBook is an account's book before any commitment.
func EmptyBook(account string) AccountBook {
	return AccountBook{
		Account:  account,
		Reserved: sdk.NewCoins(),
	}
}

// Reserve adds principal to the book, at most 128 bits per denomination.
func (b *AccountBook) Reserve(c sdk.Coin) error {
	sum, err := b.Reserved.AmountOf(c.Denom).SafeAdd(c.Amount)
	if err != nil {
		return fmt.Errorf("%s reservations: %w", c.Denom, err)
	}
	if err := chain.ValidateAmount(sum, false); err != nil {
		return fmt.Errorf("%s reservations: %w", c.Denom, err)
	}
	b.Reserved = b.Reserved.Add(c)
	return nil
}

// Release takes principal out of the book.
func (b *AccountBook) Release(c sdk.Coin) error {
	rest, negative := b.Reserved.SafeSub(c)
	if negative {
		return fmt.Errorf("%s reservations are below %s", c.Denom, c)
	}
	b.Reserved = rest
	return nil
}

// Uncommitted is custody outside the book's reservations in balance's denomination: the pool for
// NOAH, custody open to the next award otherwise. A balance below the reservations is a backing
// failure.
func (b AccountBook) Uncommitted(balance sdk.Coin) (math.Int, error) {
	held := b.Reserved.AmountOf(balance.Denom)
	if balance.Amount.LT(held) {
		return math.Int{}, fmt.Errorf("%s %s custody is below its commitments", b.Account, balance.Denom)
	}
	return balance.Amount.Sub(held), nil
}

// Validate checks a book names an account and is bounded; import checks it against the account's Bank balance.
func (b AccountBook) Validate() error {
	if !slices.Contains(PoolAccountNames(), b.Account) {
		return fmt.Errorf("%q is not a disbursement account", b.Account)
	}
	if err := b.Reserved.Validate(); err != nil {
		return fmt.Errorf("reserved: %w", err)
	}
	for _, c := range b.Reserved {
		if err := chain.ValidateAmount(c.Amount, true); err != nil {
			return fmt.Errorf("reserved %s: %w", c.Denom, err)
		}
		if b.Account == MembersName && c.Denom != chain.NoahBaseDenom {
			return errors.New("the member book holds NOAH only")
		}
	}
	return nil
}
