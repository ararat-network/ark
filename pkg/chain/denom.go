package chain

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"
)

const (
	NoahBaseDenom = "anoah"
	USDBaseDenom  = "ausd"
	KRWBaseDenom  = "akrw"
	// XDRBaseDenom is the IMF Special Drawing Right under its ISO 4217 code.
	// It is the protocol reference: the one denom with a feed and no asset.
	XDRBaseDenom = "axdr"
	CNYBaseDenom = "acny"
	JPYBaseDenom = "ajpy"
	EURBaseDenom = "aeur"
	GBPBaseDenom = "agbp"
	CADBaseDenom = "acad"
	AUDBaseDenom = "aaud"
	SGDBaseDenom = "asgd"
	MXNBaseDenom = "amxn"

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

// pricedDenomShape documents the canonical grammar for errors. isPricedDenom implements it as an
// ASCII byte scan on transaction and block paths.
const pricedDenomShape = `^a[a-z0-9]{2,15}$`

// isPricedDenom enforces pricedDenomShape as an exact ASCII byte scan. Non-ASCII and malformed
// UTF-8 bytes fail the same character rules. Differential tests compare it with the documented
// pattern.
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

// ValidatePricedDenom accepts canonical Ark feed/asset denominations and excludes NOAH, the
// numeraire. Callers supporting NOAH handle NoahBaseDenom explicitly.
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

// ExternalSeparator separates a claim's feed prefix and tag. Priced denominations forbid it while
// external symbols require it, keeping the namespaces disjoint by shape.
const ExternalSeparator = "-"

// MaxExternalTagBytes bounds the tag distinguishing external symbols that price through one
// feed — one custodian's gold from another's.
const MaxExternalTagBytes = 12

// MaxExternalDenomBytes is the longest external symbol the rule admits. It is
// deliberately not MaxPricedDenomBytes: that bound sizes vote-extension
// capacity, and an external symbol never reaches the Oracle.
const MaxExternalDenomBytes = MaxPricedDenomBytes + len(ExternalSeparator) + MaxExternalTagBytes

// externalDenomShape states the rule ExternalFeed and ValidateExternalDenom enforce
// between them, carried for error messages and documentation exactly as
// pricedDenomShape is.
const externalDenomShape = `^a[a-z0-9]{2,15}-[a-z0-9]{1,12}$`

// isExternalTag bounds the tag to the priced-denom charset, which is what makes a
// second separator a rejection rather than a longer tag: the cut below takes
// the first separator, so anything after a second one lands here and fails.
func isExternalTag(tag string) bool {
	if len(tag) == 0 || len(tag) > MaxExternalTagBytes {
		return false
	}
	for i := 0; i < len(tag); i++ {
		if c := tag[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}

	return true
}

// ExternalFeed derives an external symbol's feed prefix solely from its name and reports whether
// the shape is valid. Multiple custody claims may share a series; lookup state cannot redirect
// them.
func ExternalFeed(denom string) (string, bool) {
	feed, tag, found := strings.Cut(denom, ExternalSeparator)
	if !found || !isExternalTag(tag) {
		return "", false
	}
	// The numeraire is excluded here for the reason it is excluded everywhere:
	// it carries no feed. An off-chain NOAH holding is therefore unnameable, which
	// is the refusal the recognition policy wants anyway.
	if !isPricedDenom(feed) || feed == NoahBaseDenom {
		return "", false
	}

	return feed, true
}

// ValidateExternalDenom accepts <feed>-<tag> symbols for Reserve positions and eligibility. The
// prefix follows ValidatePricedDenom; the separator prevents native asset registration. External
// symbols are not admitted to Reserve Bank custody.
func ValidateExternalDenom(denom string) error {
	feed, tag, found := strings.Cut(denom, ExternalSeparator)
	if !found {
		return fmt.Errorf(
			"external denom must name a feed and a tag matching %s: %s",
			externalDenomShape,
			denom,
		)
	}
	if err := ValidatePricedDenom(feed); err != nil {
		return fmt.Errorf("external denom %s prices through an invalid feed: %w", denom, err)
	}
	if !isExternalTag(tag) {
		return fmt.Errorf(
			"external denom must carry one tag of at most %d lowercase alphanumeric bytes, matching %s: %s",
			MaxExternalTagBytes,
			externalDenomShape,
			denom,
		)
	}

	return nil
}
