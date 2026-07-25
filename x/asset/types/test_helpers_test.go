package types_test

import (
	"strings"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

func testAsset(denom string) assettypes.Asset {
	display := denom[1:]
	return assettypes.Asset{
		Denom: denom,
		Metadata: banktypes.Metadata{
			Description: "A test asset.",
			DenomUnits: []*banktypes.DenomUnit{
				{Denom: denom, Exponent: 0},
				{Denom: display, Exponent: chain.NativeDisplayExponent},
			},
			Base:    denom,
			Display: display,
			Name:    strings.ToUpper(display),
			Symbol:  strings.ToUpper(display),
		},
		Status:         assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
		Version:        1,
		OracleRequired: true,
	}
}
