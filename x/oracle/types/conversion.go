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
// cannot see. Errors are reserved for unusable offer amounts, denominations
// missing from the set, and arithmetic that leaves representable range. A
// malformed denomination is answered as a missing one rather than as bad input:
// the set is the authority on what can be converted, and nothing outside it is
// convertible whatever its shape.
func (r RateSet) Convert(offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	if offerCoin.Amount.IsNil() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not set", offerCoin.Denom)
	}
	if offerCoin.Amount.IsNegative() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "negative offer amount: %s", offerCoin)
	}
	if !offerCoin.Amount.IsInValidRange() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not representable", offerCoin.Denom)
	}

	// Membership in the set admits a denomination, and it is also what vouches
	// for it. Every key in a rate set arrives from a source the chain already
	// gated with chain.ValidatePricedDenom — the exchange-rate store, the feed
	// list, the asset registry, a settlement plan, params — or is the numeraire
	// constant, and that rule is strictly tighter than the SDK's denomination
	// charset. A denomination found here therefore cannot be malformed, and
	// re-deriving that with a regular expression costs more than the conversion
	// arithmetic it would be guarding.
	//
	// What the invariant needs is that no denomination leaves this function
	// without having been proven a key, so nothing may return one before both
	// lookups. That is why the identity case is answered between them rather
	// than ahead of them: an offer denomination absent from the set is unknown
	// even when it is also the ask, and returning it early would be the one path
	// that escapes the proof.
	offerRate, ok := r[offerCoin.Denom]
	if !ok {
		return sdk.DecCoin{}, sdkerrors.Wrap(ErrUnknownDenom, offerCoin.Denom)
	}
	if offerCoin.Denom == askDenom {
		return offerCoin, nil
	}
	if offerCoin.Amount.IsZero() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "zero offer amount: %s", offerCoin)
	}

	askRate, ok := r[askDenom]
	if !ok {
		return sdk.DecCoin{}, sdkerrors.Wrap(ErrUnknownDenom, askDenom)
	}

	convertedAmount := offerCoin.Amount
	if !isOne(askRate) {
		var err error
		convertedAmount, err = decimal.Mul(offerCoin.Amount, askRate)
		if err != nil {
			return sdk.DecCoin{}, sdkerrors.Wrapf(
				ErrConversionOutOfRange,
				"multiplying %s amount by %s rate: %v",
				offerCoin.Denom,
				askDenom,
				err,
			)
		}
	}

	amount := convertedAmount
	if !isOne(offerRate) {
		var err error
		amount, err = decimal.Quo(convertedAmount, offerRate)
		if err != nil {
			return sdk.DecCoin{}, sdkerrors.Wrapf(
				ErrConversionOutOfRange,
				"dividing converted amount by %s rate: %v",
				offerCoin.Denom,
				err,
			)
		}
	}
	if amount.IsNegative() {
		return sdk.DecCoin{}, sdkerrors.Wrapf(ErrConversionOutOfRange, "conversion of %s to %s is negative", offerCoin, askDenom)
	}

	return sdk.DecCoin{Denom: askDenom, Amount: amount}, nil
}

// oneDec is the comparand isOne tests against, held once rather than rebuilt
// per call: allocating a LegacyDec to decide whether arithmetic can be skipped
// would spend a good part of what the skip saves. Nothing mutates it, because
// Equal compares through big.Int.Cmp.
var oneDec = math.LegacyOneDec()

// isOne reports whether rate is exactly one, and is the guard for skipping a
// conversion leg.
func isOne(rate math.LegacyDec) bool {
	return !rate.IsNil() && rate.Equal(oneDec)
}
