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

// NativeAssetMetadata derives immutable Bank metadata from a validated priced denomination. Display
// units strip the base prefix; descriptions are asset-class neutral. Retirement retains metadata
// and prevents denomination reuse.
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
		Symbol:  fmt.Sprintf("ark%s", strings.ToUpper(display)),
	}
}
