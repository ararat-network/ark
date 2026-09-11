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

// currencyNames names the reference currency behind each launch denomination for its Bank
// description. Stored metadata is checked against this derivation at import, so an entry may be
// added only before its denomination is registered and never changed afterwards.
var currencyNames = map[string]string{
	AUDBaseDenom: "Australian dollar",
	CADBaseDenom: "Canadian dollar",
	CNYBaseDenom: "Chinese yuan",
	EURBaseDenom: "euro",
	GBPBaseDenom: "pound sterling",
	JPYBaseDenom: "Japanese yen",
	KRWBaseDenom: "South Korean won",
	MXNBaseDenom: "Mexican peso",
	SGDBaseDenom: "Singapore dollar",
	USDBaseDenom: "United States dollar",
}

// NativeAssetMetadata derives immutable Bank metadata from a validated priced denomination. Display
// units strip the base prefix; a denomination without a currency name gets a neutral description.
// Retirement retains metadata and prevents denomination reuse.
func NativeAssetMetadata(denom string) banktypes.Metadata {
	display := denom[1:]
	description := "An Ark currency."
	if name, named := currencyNames[denom]; named {
		description = fmt.Sprintf("An Ark currency tracking the %s.", name)
	}

	return banktypes.Metadata{
		Description: description,
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
