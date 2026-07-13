package types

import (
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/pkg/decimal"
)

// RateSnapshot is an in-memory set of oracle rates used for one conversion flow.
type RateSnapshot map[string]math.LegacyDec

// Convert converts an offer coin into the ask denom using the captured rates.
func (r RateSnapshot) Convert(offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	if offerCoin.Amount.IsNil() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not set", offerCoin.Denom)
	}
	if err := offerCoin.Validate(); err != nil {
		return sdk.DecCoin{}, sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "invalid offer coin: %v", err)
	}
	if !offerCoin.Amount.IsInValidRange() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not representable", offerCoin.Denom)
	}
	if err := sdk.ValidateDenom(askDenom); err != nil {
		return sdk.DecCoin{}, sdkerrors.Wrapf(errortypes.ErrInvalidRequest, "invalid ask denom %q: %v", askDenom, err)
	}
	if offerCoin.Denom == askDenom {
		return offerCoin, nil
	}
	if !offerCoin.IsPositive() {
		return sdk.DecCoin{}, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}

	offerRate, ok := r[offerCoin.Denom]
	if !ok {
		return sdk.DecCoin{}, sdkerrors.Wrap(ErrUnknownDenom, offerCoin.Denom)
	}
	if offerRate.IsNil() || !offerRate.IsInValidRange() || !offerRate.IsPositive() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrInvalidExchangeRate, "%s rate %s", offerCoin.Denom, formatRate(offerRate))
	}

	askRate, ok := r[askDenom]
	if !ok {
		return sdk.DecCoin{}, sdkerrors.Wrap(ErrUnknownDenom, askDenom)
	}
	if askRate.IsNil() || !askRate.IsInValidRange() || !askRate.IsPositive() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrInvalidExchangeRate, "%s rate %s", askDenom, formatRate(askRate))
	}

	convertedAmount, err := decimal.Mul(offerCoin.Amount, askRate)
	if err != nil {
		return sdk.DecCoin{}, sdkerrors.Wrapf(
			ErrConversionOutOfRange,
			"multiplying %s amount by %s rate: %v",
			offerCoin.Denom,
			askDenom,
			err,
		)
	}
	amount, err := decimal.Quo(convertedAmount, offerRate)
	if err != nil {
		return sdk.DecCoin{}, sdkerrors.Wrapf(
			ErrConversionOutOfRange,
			"dividing converted amount by %s rate: %v",
			offerCoin.Denom,
			err,
		)
	}
	if !amount.IsPositive() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrConversionOutOfRange, "conversion of %s to %s rounded to zero", offerCoin, askDenom)
	}

	return sdk.NewDecCoinFromDec(askDenom, amount), nil
}

func formatRate(rate math.LegacyDec) string {
	if rate.IsNil() {
		return "<nil>"
	}
	return fmt.Sprint(rate)
}
