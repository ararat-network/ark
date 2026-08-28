package types_test

import (
	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
)

func testAsset(denom string) assettypes.Asset {
	return assettypes.Asset{
		Denom:    denom,
		Metadata: chain.NativeAssetMetadata(denom),
		Status:   assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
		Version:  1,
	}
}
