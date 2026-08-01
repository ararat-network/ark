package types

import (
	"maps"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
)

// RateSet is an in-memory set of oracle rates used for one conversion flow.
type RateSet map[string]math.LegacyDec

// NewRateSet returns a rate set carrying the NOAH identity and nothing else.
// Every rate set carries it: NOAH is the numeraire every oracle rate is quoted
// against, so a set without it cannot convert to or from the unit every
// conversion routes through, and the rate is one by definition rather than by
// observation.
func NewRateSet() RateSet {
	return NewRateSetFrom(nil)
}

// NewRateSetFrom returns a rate set carrying the NOAH identity over a copy of
// rates. The copy is the point: callers hand their own maps across module
// boundaries, and a set that grew a borrowed map would mutate state another
// module is still reading. The identity is written last, so a NOAH entry in
// rates is discarded rather than overriding a rate that is one by definition.
func NewRateSetFrom(rates map[string]math.LegacyDec) RateSet {
	set := make(RateSet, len(rates)+1)
	maps.Copy(set, rates)
	set[chain.NoahBaseDenom] = math.LegacyOneDec()
	return set
}

// Convert converts an offer coin into the ask denom using the captured rates.
//
// Zero is a valid result: a positive offer whose converted value sits below
// Dec precision truncates to zero rather than failing, because whether nothing
// is an acceptable outcome is the caller's policy, not the conversion's.
// Valuations count such dust as exactly what it is worth, while callers paying
// out an entitlement must refuse a non-positive payout after truncating to
// whole units — the granularity bar that actually matters, which this function
// cannot see. Errors are reserved for invalid inputs, denominations missing
// from the set, and arithmetic that leaves representable range.
func (r RateSet) Convert(offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
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

	askRate, ok := r[askDenom]
	if !ok {
		return sdk.DecCoin{}, sdkerrors.Wrap(ErrUnknownDenom, askDenom)
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
	// Rate sets are plain maps whose values this function never validates; a
	// negative rate would yield a negative amount, which NewDecCoinFromDec
	// panics on.
	if amount.IsNegative() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrConversionOutOfRange, "conversion of %s to %s is negative", offerCoin, askDenom)
	}

	return sdk.NewDecCoinFromDec(askDenom, amount), nil
}
