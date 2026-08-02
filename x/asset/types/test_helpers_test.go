package types_test

import (
	"ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

func testAsset(denom string) assettypes.Asset {
	return assettypes.Asset{
		Denom:    denom,
		Metadata: chain.NativeAssetMetadata(denom),
		Status:   assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
		Version:  1,
	}
}
