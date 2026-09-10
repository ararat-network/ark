package chain

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// ValidateNoahOnlyDeposit requires exactly one positive NOAH coin. Modules apply this shared
// predicate to their own custody accounts; fundLabel identifies the refusing account set.
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

// ValidateNoahCoin checks a well-formed NOAH coin, including zero. Callers that require positivity
// must enforce it separately; errors identify field.
func ValidateNoahCoin(field string, coin sdk.Coin) error {
	if err := coin.Validate(); err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	if coin.Denom != NoahBaseDenom {
		return fmt.Errorf("%s must be denominated in %s", field, NoahBaseDenom)
	}

	return nil
}

// NoahCoin constructs a NOAH coin and panics on nil or negative amounts, matching SDK semantics.
// The constant denomination needs no regex check; caller-supplied denominations still require SDK
// validation.
func NoahCoin(amount math.Int) sdk.Coin {
	if amount.IsNil() {
		panic("NOAH coin amount is not set")
	}
	if amount.IsNegative() {
		panic(fmt.Sprintf("negative NOAH coin amount: %s", amount))
	}

	return sdk.Coin{Denom: NoahBaseDenom, Amount: amount}
}

// NoahCoins returns a single NOAH coin, or an empty set for zero. It preserves SDK amount checks
// and zero removal without redundant singleton sorting or constant-denomination validation.
func NoahCoins(amount math.Int) sdk.Coins {
	coin := NoahCoin(amount)
	if coin.Amount.IsZero() {
		return sdk.Coins{}
	}

	return sdk.Coins{coin}
}

// NoahDecCoin constructs a decimal NOAH coin without revalidating its constant denomination. Nil or
// negative amounts panic as in sdk.NewDecCoinFromDec.
func NoahDecCoin(amount math.LegacyDec) sdk.DecCoin {
	if amount.IsNil() {
		panic("NOAH decimal coin amount is not set")
	}
	if amount.IsNegative() {
		panic(fmt.Sprintf("negative NOAH decimal coin amount: %s", amount))
	}

	return sdk.DecCoin{Denom: NoahBaseDenom, Amount: amount}
}
