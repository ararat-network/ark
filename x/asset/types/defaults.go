package types

import (
	"fmt"
	"strings"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "ark/pkg/chain"
)

// DefaultAssets returns the existing launch assets in canonical denom order.
func DefaultAssets() []Asset {
	denoms := []string{
		chain.CNYBaseDenom,
		chain.EURBaseDenom,
		chain.GBPBaseDenom,
		chain.JPYBaseDenom,
		chain.KRWBaseDenom,
		chain.MNTBaseDenom,
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	}
	assets := make([]Asset, len(denoms))
	for i, denom := range denoms {
		assets[i] = Asset{
			Denom:          denom,
			Metadata:       defaultAssetMetadata(denom),
			Status:         AssetStatus_ASSET_STATUS_ACTIVE,
			Version:        InitialOracleTargetVersion,
			OracleRequired: true,
		}
	}

	return assets
}

func defaultAssetMetadata(denom string) banktypes.Metadata {
	display := denom[1:]
	return banktypes.Metadata{
		Description: "The native stable token of Ark Icarus.",
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: denom, Exponent: 0},
			{Denom: display, Exponent: chain.NativeDisplayExponent},
		},
		Base:    denom,
		Display: display,
		Name:    fmt.Sprintf("%s ARK", strings.ToUpper(display)),
		Symbol:  fmt.Sprintf("%sA", strings.ToUpper(display[:len(display)-1])),
	}
}
