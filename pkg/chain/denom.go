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

// ExternalSeparator divides an external symbol's feed prefix from its tag. It is the
// one byte the priced-denom rule has never admitted, which is what makes the
// external and Ark-issued namespaces disjoint by shape rather than by state: no
// registration can accept a name containing it, and no eligibility entry may
// omit it.
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

// ExternalFeed returns the feed key an external symbol prices through — the prefix
// before the separator — and reports whether denom is external-shaped at all.
//
// The derivation is unconditional: an external symbol names its series in its own
// text, for every reader, forever. It is never resolved against what feeds or
// assets happen to exist, because a conditional resolution would silently
// re-point every external symbol on a series the moment a feed appeared, which is the
// class of hazard the partition exists to remove.
//
// Many external symbols may share one feed. Two custodians holding the same
// instrument are two symbols — different counterparty risk, so different
// haircuts — over one series the validators vote once.
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

// ValidateExternalDenom validates denom as an external symbol: an external holding the
// Reserve attests to, named `<feed>-<tag>`.
//
// This is the second and last denomination rule the chain has, and it is
// bounded to one module by construction: an external symbol keys an eligibility
// entry, a position, and the custody either accounts for.
//
// What it can never be is protocol paper. The priced shape refuses the
// separator, so an external symbol cannot be registered, and conversion — the only
// native mint path — mints registry members alone. It can still travel as an
// SDK denomination, whose charset does admit the separator, so custody the
// chain does hold under such a name is ordinary bank state, counted beside the
// attested holdings rather than instead of them.
//
// The prefix rule is ValidatePricedDenom itself rather than a copy of it, so a
// external symbol's feed key is a legal feed key by construction and every refusal the
// priced rule makes — the numeraire, punctuation, the length bound — is made
// here in the same words.
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
