package chain

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// ValidateNoahOnlyDeposit admits exactly one positive NOAH coin into a
// protocol custody account, and is the shared predicate behind every module's
// fund send restriction. Each module registers its own restriction over its own
// accounts; only this rule is common to them.
//
// fundLabel names the refusing account set in the error, so a rejected transfer
// says which restriction stopped it.
//
// The rule is deliberately blunt because every launch fund is NOAH-denominated
// (ECONOMIC_DESIGN.md §7.3). It stops being common the moment a fund
// gains a custody allowlist under §20.1, at which point that fund's module
// widens its own restriction and stops calling this.
func ValidateNoahOnlyDeposit(fundLabel string, amount sdk.Coins) error {
	if len(amount) != 1 ||
		amount[0].Denom != NoahBaseDenom ||
		amount[0].Amount.IsNil() ||
		!amount[0].Amount.IsPositive() {
		return errorsmod.Wrapf(
			errortypes.ErrInvalidCoins,
			"%s deposits must contain exactly one positive %s coin",
			fundLabel,
			NoahBaseDenom,
		)
	}

	return nil
}

// ValidateNoahCoin admits a well-formed NOAH coin, naming field in the error.
//
// Coin.Validate already rejects an unset or negative amount, so this is the
// whole shape rule and callers can read the amount directly afterwards. It
// deliberately says nothing about positivity: zero is a legitimate NOAH
// quantity in stored records — an unused allowance, an unrecovered position —
// and the call sites that must reject it require it only conditionally, so
// they assert it themselves.
func ValidateNoahCoin(field string, coin sdk.Coin) error {
	if err := coin.Validate(); err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	if coin.Denom != NoahBaseDenom {
		return fmt.Errorf("%s must be denominated in %s", field, NoahBaseDenom)
	}

	return nil
}

// NoahCoin returns amount denominated in NOAH.
//
// The denomination is not checked because it is a compile-time constant: there
// is nothing about NoahBaseDenom a check could discover, and what sdk.NewCoin
// would add here is a regular-expression match over a string literal, on paths
// that run it per block and per transaction. That reasoning is deliberately
// not extended to caller-supplied denominations — the SDK constructors' match
// is the last defence between an upstream bug and a malformed denomination
// persisted into bank state, so it is only ever skipped where the denomination
// is a constant and the check is provably vacuous. The amount is still
// checked, because the amount is the part a caller can get wrong.
//
// It panics on an unset or negative amount exactly as sdk.NewCoin does. A coin
// is a value whose invariants hold by construction, and code that has computed
// a negative protocol balance has a defect rather than a condition to handle.
func NoahCoin(amount math.Int) sdk.Coin {
	if amount.IsNil() {
		panic("NOAH coin amount is not set")
	}
	if amount.IsNegative() {
		panic(fmt.Sprintf("negative NOAH coin amount: %s", amount))
	}

	return sdk.Coin{Denom: NoahBaseDenom, Amount: amount}
}

// NoahCoins returns amount as a one-coin NOAH set, or the empty set when amount
// is zero.
//
// Dropping zero is the one thing sdk.NewCoins does here that is not redundant,
// so it survives: consumers of a coin set treat a zero entry as malformed
// rather than as nothing, and bank rejects one outright. The sorting does not
// survive, because a single coin is sorted, and neither does the denomination
// match, for the reason given on NoahCoin.
func NoahCoins(amount math.Int) sdk.Coins {
	coin := NoahCoin(amount)
	if coin.Amount.IsZero() {
		return sdk.Coins{}
	}

	return sdk.Coins{coin}
}

// NoahDecCoin returns amount denominated in NOAH, for the decimal quantities
// valuations carry before they are truncated to whole units. The denomination
// goes unchecked for the reason given on NoahCoin.
//
// It panics on an unset or negative amount, as sdk.NewDecCoinFromDec does.
func NoahDecCoin(amount math.LegacyDec) sdk.DecCoin {
	if amount.IsNil() {
		panic("NOAH decimal coin amount is not set")
	}
	if amount.IsNegative() {
		panic(fmt.Sprintf("negative NOAH decimal coin amount: %s", amount))
	}

	return sdk.DecCoin{Denom: NoahBaseDenom, Amount: amount}
}
