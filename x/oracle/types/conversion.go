package types

import (
	"maps"
	"time"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
)

// RateSet is an in-memory set of oracle rates used for one conversion flow.
// Each rate is NOAH per one unit of its denomination, so a unit's NOAH value
// is a multiplication and the numeraire's own rate is one.
type RateSet map[string]math.LegacyDec

// NewRateSet returns a set containing only NOAH at one, the identity required by every conversion
// through the numeraire.
func NewRateSet() RateSet {
	return NewRateSetFrom(nil)
}

// NewRateSetFrom copies rates and forces NOAH identity to one. It never mutates the caller's map or
// accepts an overridden numeraire rate.
func NewRateSetFrom(rates map[string]math.LegacyDec) RateSet {
	set := make(RateSet, len(rates)+1)
	maps.Copy(set, rates)
	set[chain.NoahBaseDenom] = math.LegacyOneDec()
	return set
}

// RateRequest supplies a denomination and consumer-specific freshness window. Its feed derives from
// the denomination's naming grammar; results retain the requested key so shared feeds can receive
// different freshness verdicts.
type RateRequest struct {
	Denom  string
	MaxAge time.Duration
}

// Convert computes offer-to-ask value from captured rates. Sub-precision results may be zero;
// payout callers must enforce their own minimum. Invalid amounts, missing denominations, and
// unrepresentable arithmetic fail. Denomination shape is vouched for by set membership.
func (r RateSet) Convert(offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	if offerCoin.Amount.IsNil() {
		return sdk.DecCoin{}, errorsmod.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not set", offerCoin.Denom)
	}
	if offerCoin.Amount.IsNegative() {
		return sdk.DecCoin{}, errorsmod.Wrapf(errortypes.ErrInvalidCoins, "negative offer amount: %s", offerCoin)
	}
	if !offerCoin.Amount.IsInValidRange() {
		return sdk.DecCoin{}, errorsmod.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not representable", offerCoin.Denom)
	}

	// Require set membership before returning any denomination, including identity conversions.
	// Trusted rate-set writers validate keys, so another denomination regex is unnecessary here.
	offerRate, ok := r[offerCoin.Denom]
	if !ok {
		return sdk.DecCoin{}, errorsmod.Wrap(ErrUnknownDenom, offerCoin.Denom)
	}
	if offerCoin.Denom == askDenom {
		return offerCoin, nil
	}
	if offerCoin.Amount.IsZero() {
		return sdk.DecCoin{}, errorsmod.Wrapf(errortypes.ErrInvalidCoins, "zero offer amount: %s", offerCoin)
	}

	askRate, ok := r[askDenom]
	if !ok {
		return sdk.DecCoin{}, errorsmod.Wrap(ErrUnknownDenom, askDenom)
	}

	// Multiply the offer by its NOAH-per-unit rate before dividing by the ask rate. The NOAH ask
	// needs no division; this order avoids rounding an intermediate rate ratio.
	noahValue := offerCoin.Amount
	if !isOne(offerRate) {
		var err error
		noahValue, err = decimal.Mul(offerCoin.Amount, offerRate)
		if err != nil {
			return sdk.DecCoin{}, errorsmod.Wrapf(
				ErrConversionOutOfRange,
				"multiplying %s amount by its rate: %v",
				offerCoin.Denom,
				err,
			)
		}
	}

	amount := noahValue
	if !isOne(askRate) {
		var err error
		amount, err = decimal.Quo(noahValue, askRate)
		if err != nil {
			return sdk.DecCoin{}, errorsmod.Wrapf(
				ErrConversionOutOfRange,
				"dividing NOAH value by %s rate: %v",
				askDenom,
				err,
			)
		}
	}
	if amount.IsNegative() {
		return sdk.DecCoin{}, errorsmod.Wrapf(ErrConversionOutOfRange, "conversion of %s to %s is negative", offerCoin, askDenom)
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
