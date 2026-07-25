package chain

import banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

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
