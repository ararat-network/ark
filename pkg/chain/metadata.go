package chain

import (
	"fmt"
	"strings"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// NoahMetadata returns the canonical Bank metadata for Ark's native staking
// denomination.
func NoahMetadata() banktypes.Metadata {
	return banktypes.Metadata{
		Description: "The native staking token of Ark.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: NoahBaseDenom, Exponent: 0},
			{Denom: "noah", Exponent: NativeDisplayExponent},
		},
		Base:    NoahBaseDenom,
		Display: "noah",
		Name:    "NOAH",
		Symbol:  "NOAH",
	}
}

// NativeAssetMetadata returns the canonical Bank metadata for a priced Ark
// asset. Every field is a function of the denomination, which is what lets
// registration derive the metadata rather than accept it: an identity that
// cannot be misspelled never needs correcting, so the record is immutable by
// construction rather than by a lifecycle rule.
//
// The denomination must already satisfy ValidatePricedDenom; the display unit
// is its base-unit prefix removed, which that rule guarantees is non-empty.
//
// The description names no asset class deliberately. One derivation serves
// every registered asset — a tokenized commodity registers exactly as a
// stablecoin does, because the axis the registry records is convertibility and
// nothing else — and the record it writes is permanent: retirement keeps the
// Bank metadata as a tombstone and registration refuses to collide with it, so
// a description that classified would misdescribe assets forever with no
// message able to correct it.
//
// Terra Classic derived the same three fields, but its symbol rule dropped the
// last letter of the currency code before appending the chain initial — usd
// became UST and krw became KRT, the tickers it shipped. That formula encoded
// those strings rather than a principle, and under a different initial it
// degrades: it would make ausd into USA. The suffix is appended whole here.
func NativeAssetMetadata(denom string) banktypes.Metadata {
	display := denom[1:]

	return banktypes.Metadata{
		Description: "A native asset of Ark Icarus.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: denom, Exponent: 0},
			{Denom: display, Exponent: NativeDisplayExponent},
		},
		Base:    denom,
		Display: display,
		Name:    fmt.Sprintf("Ark%s", strings.ToUpper(display)),
		Symbol:  fmt.Sprintf("%sA", strings.ToUpper(display)),
	}
}
