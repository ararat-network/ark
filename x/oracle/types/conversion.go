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

// NewRateSet returns a rate set carrying the NOAH identity and nothing else.
// Every rate set carries it: NOAH is the numeraire every oracle rate is quoted
// in, so a set without it cannot convert to or from the unit every conversion
// routes through, and the rate is one by definition rather than by
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

// RateRequest asks for one denomination's rate under the caller's own
// staleness window.
//
// The window is a per-call argument rather than Oracle state, which is the
// whole design: how often a series publishes is a fact about the feed, but how
// old a rate may be and still back a decision is a fact about the decision,
// and one feed now answers to consumers with different tolerances. Two
// requests may name one series under different windows — the Reserve sends one
// per eligibility entry — and each verdict comes back under the requested
// denomination, keyed apart.
//
// The request deliberately does not say which feed to read. The denomination
// prices through the feed its own name derives — its prefix when it is an
// external symbol, itself when it is a feed key — and the derivation is the
// chain's naming grammar, pure and available to the Oracle in pkg/chain. A
// feed field would state what the name already states, and could state it
// wrongly, routing a symbol to another series with the Oracle unable to tell.
type RateRequest struct {
	Denom  string
	MaxAge time.Duration
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
		return sdk.DecCoin{}, errorsmod.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not set", offerCoin.Denom)
	}
	if offerCoin.Amount.IsNegative() {
		return sdk.DecCoin{}, errorsmod.Wrapf(errortypes.ErrInvalidCoins, "negative offer amount: %s", offerCoin)
	}
	if !offerCoin.Amount.IsInValidRange() {
		return sdk.DecCoin{}, errorsmod.Wrapf(ErrConversionOutOfRange, "offer amount for %s is not representable", offerCoin.Denom)
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

	// Value the offer in NOAH first: the offer rate is NOAH per one offer
	// unit, so the product is the offer's NOAH value, and a conversion into
	// NOAH ends there with no division at all. The ask leg then divides that
	// value by NOAH per one ask unit. Multiplying first leaves the one
	// rounding on the quotient.
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
