package chain

import (
	"fmt"

	"cosmossdk.io/math"
)

const (
	NoahBaseDenom = "anoah"
	USDBaseDenom  = "ausd"
	KRWBaseDenom  = "akrw"
	SDRBaseDenom  = "asdr"
	CNYBaseDenom  = "acny"
	JPYBaseDenom  = "ajpy"
	EURBaseDenom  = "aeur"
	GBPBaseDenom  = "agbp"
	MNTBaseDenom  = "amnt"

	NativeDisplayExponent = 18
)

// NativeBaseAmount converts a whole Ark-native display amount into base units.
func NativeBaseAmount(wholeUnits int64) math.Int {
	return math.NewIntWithDecimal(wholeUnits, NativeDisplayExponent)
}

// MaxPricedDenomBytes is the longest denomination the priced-denom rule admits:
// the base-unit prefix plus fifteen symbol characters. Vote-extension capacity
// limits derive from this bound.
const MaxPricedDenomBytes = 16

// minPricedDenomBytes is the shortest the rule admits: the base-unit prefix
// plus two symbol characters. Three keeps every priced denomination a valid SDK
// denomination too, which is what lets rates travel as DecCoins.
const minPricedDenomBytes = 3

// pricedDenomShape states the rule isPricedDenom enforces, in the notation the
// rule was written in. It is carried for error messages and documentation only:
// the match itself is a byte scan, because ValidatePricedDenom sits on
// per-block and per-transaction paths where the regexp engine costs an order of
// magnitude more than the comparison it is performing.
const pricedDenomShape = `^a[a-z0-9]{2,15}$`

// isPricedDenom bounds a priced denomination to a short lowercase alphanumeric
// symbol behind the base-unit prefix, as pricedDenomShape describes. It is
// deliberately tighter than the SDK's denomination charset: a denomination is
// also the key of the oracle feed pricing it, so admitting punctuation here
// would produce denominations that could never carry a feed.
//
// Scanning bytes decides the shape exactly rather than approximately. The
// character class is ASCII-only, so a denomination that matches has one byte
// per character and its byte length is its length; any byte outside the class,
// including every byte of a multi-byte or malformed rune, is rejected here for
// the same reason the pattern rejects it. denom_internal_test.go holds the
// pattern and asserts the two agree.
func isPricedDenom(denom string) bool {
	if len(denom) < minPricedDenomBytes || len(denom) > MaxPricedDenomBytes {
		return false
	}
	if denom[0] != 'a' {
		return false
	}
	for i := 1; i < len(denom); i++ {
		if c := denom[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}

	return true
}

// ValidatePricedDenom validates denom as a canonical Ark-native denomination
// the protocol prices against NOAH.
//
// This is the one denomination rule the chain has, because everything that
// validates a denomination is validating something that must be able to carry
// a feed: a registered asset, a settlement plan or write-off record for one, a
// Tobin entry, a tax cap, a resolver route, the protocol reference. The
// numeraire is excluded for the reason it has no feed — NOAH is not priced, it
// is what prices everything else. Code that legitimately handles NOAH compares
// against NoahBaseDenom directly rather than validating.
func ValidatePricedDenom(denom string) error {
	if !isPricedDenom(denom) {
		return fmt.Errorf(
			"denom must be an Ark-native base denom matching %s: %s",
			pricedDenomShape,
			denom,
		)
	}
	if denom == NoahBaseDenom {
		return fmt.Errorf("denom %s is the numeraire and is never priced", denom)
	}

	return nil
}
