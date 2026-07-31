package chain

import (
	"fmt"
	"regexp"

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

// MaxPricedDenomBytes is the longest denomination pricedDenomPattern admits:
// the base-unit prefix plus fifteen symbol characters. Vote-extension capacity
// limits derive from this bound.
const MaxPricedDenomBytes = 16

// pricedDenomPattern bounds a priced denomination to a short lowercase
// alphanumeric symbol behind the base-unit prefix. It is deliberately tighter
// than the SDK's denomination charset: a denomination is also the key of the
// oracle feed pricing it, so admitting punctuation here would produce
// denominations that could never carry a feed. The minimum length of three
// keeps every one of them a valid SDK denomination too, which is what lets
// rates travel as DecCoins.
var pricedDenomPattern = regexp.MustCompile(`^a[a-z0-9]{2,15}$`)

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
	if !pricedDenomPattern.MatchString(denom) {
		return fmt.Errorf(
			"denom must be an Ark-native base denom matching %s: %s",
			pricedDenomPattern.String(),
			denom,
		)
	}
	if denom == NoahBaseDenom {
		return fmt.Errorf("denom %s is the numeraire and is never priced", denom)
	}

	return nil
}
