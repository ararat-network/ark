package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// EmptyBook is an account's book before any commitment.
func EmptyBook(account string) AccountBook {
	return AccountBook{
		Pool:     account,
		Reserved: sdk.NewCoins(),
		Open:     math.ZeroInt(),
	}
}

// Reserve adds principal to the book, at most 128 bits per denomination.
func (b *AccountBook) Reserve(c sdk.Coin) error {
	sum, err := b.Reserved.AmountOf(c.Denom).SafeAdd(c.Amount)
	if err != nil {
		return fmt.Errorf("%s reservations: %w", c.Denom, err)
	}
	if err := ValidateAmount(sum, false); err != nil {
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

// Validate checks a book names an account and is bounded; import checks it against the account's Bank balance.
func (b AccountBook) Validate() error {
	if err := ValidateAccount(b.Pool); err != nil {
		return err
	}
	if err := b.Reserved.Validate(); err != nil {
		return fmt.Errorf("reserved: %w", err)
	}
	for _, c := range b.Reserved {
		if err := ValidateAmount(c.Amount, true); err != nil {
			return fmt.Errorf("reserved %s: %w", c.Denom, err)
		}
		if b.Pool == MembersName && c.Denom != chain.NoahBaseDenom {
			return errors.New("the member book holds NOAH only")
		}
	}
	if err := ValidateAmount(b.Open, false); err != nil {
		return err
	}
	if b.Pool == ContributorsName && !b.Open.IsZero() {
		return errors.New("the contributor book has no tranche")
	}
	return nil
}
