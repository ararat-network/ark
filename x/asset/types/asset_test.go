package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

func TestAssetValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*assettypes.Asset)
		expectErr string
	}{
		{
			name: "valid priced asset",
		},
		{
			name: "valid unpriced active asset",
			mutate: func(asset *assettypes.Asset) {
				asset.OracleRequired = false
			},
		},
		{
			name: "valid delisted priced asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_DELISTED
			},
		},
		{
			name: "valid written-off priced asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
			},
		},
		{
			name: "invalid denom",
			mutate: func(asset *assettypes.Asset) {
				asset.Denom = "aGOLD"
				asset.Metadata.Base = "aGOLD"
				asset.Metadata.DenomUnits[0].Denom = "aGOLD"
			},
			expectErr: "canonical lowercase Ark-native base denom",
		},
		{
			name: "native denom",
			mutate: func(asset *assettypes.Asset) {
				asset.Denom = chain.NoahBaseDenom
				asset.Metadata.Base = chain.NoahBaseDenom
				asset.Metadata.DenomUnits[0].Denom = chain.NoahBaseDenom
			},
			expectErr: "must not use native denom anoah",
		},
		{
			name: "metadata base mismatch",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Base = "asilver"
			},
			expectErr: "must match asset denom",
		},
		{
			name: "nil denomination unit",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits[1] = nil
			},
			expectErr: "denomination unit 1 must not be nil",
		},
		{
			name: "missing display denomination unit",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits = asset.Metadata.DenomUnits[:1]
			},
			expectErr: "must contain at least base and display denomination units",
		},
		{
			name: "missing display unit",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Display = "gold-token"
			},
			expectErr: "metadata must contain a denomination unit",
		},
		{
			name: "nonstandard display denom",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Display = "gold-token"
				asset.Metadata.DenomUnits[1].Denom = "gold-token"
			},
			expectErr: "display denom must be gold",
		},
		{
			name: "nonstandard display exponent",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits[1].Exponent = 6
			},
			expectErr: "display exponent must be 18",
		},
		{
			name: "valid intermediate denomination unit",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits = []*banktypes.DenomUnit{
					asset.Metadata.DenomUnits[0],
					&banktypes.DenomUnit{Denom: "milligold", Exponent: 15},
					asset.Metadata.DenomUnits[1],
				}
			},
		},
		{
			name: "denomination alias",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits[0].Aliases = []string{"attogold"}
			},
			expectErr: "must not define aliases",
		},
		{
			name: "unspecified status",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED
			},
			expectErr: "status must be specified and known",
		},
		{
			name: "unknown status",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus(99)
			},
			expectErr: "status must be specified and known",
		},
		{
			name: "zero version",
			mutate: func(asset *assettypes.Asset) {
				asset.Version = 0
			},
			expectErr: "version must be positive",
		},
		{
			name: "unpriced removal pending",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_REMOVAL_PENDING
				asset.OracleRequired = false
			},
			expectErr: "asset must require Oracle pricing",
		},
		{
			name: "unpriced delisted asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_DELISTED
				asset.OracleRequired = false
			},
			expectErr: "asset must require Oracle pricing",
		},
		{
			name: "unpriced settling asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_SETTLING
				asset.OracleRequired = false
			},
			expectErr: "asset must require Oracle pricing",
		},
		{
			name: "unpriced written-off asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
				asset.OracleRequired = false
			},
			expectErr: "asset must require Oracle pricing",
		},
		{
			name: "unpriced delisting asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_DELISTING
				asset.OracleRequired = false
			},
			expectErr: "asset must require Oracle pricing",
		},
		{
			name: "unpriced relisting asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_RELISTING
				asset.OracleRequired = false
			},
			expectErr: "asset must require Oracle pricing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset := testAsset("agold")
			if tt.mutate != nil {
				tt.mutate(&asset)
			}

			err := asset.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}

func TestAssetLockValidate(t *testing.T) {
	tests := []struct {
		name      string
		lock      assettypes.AssetLock
		expectErr string
	}{
		{
			name: "valid",
			lock: assettypes.AssetLock{
				Denom: "agold",
				Kind:  assettypes.AssetLockKind_ASSET_LOCK_KIND_MARKET_ASSET_POLICY,
			},
		},
		{
			name: "native denom",
			lock: assettypes.AssetLock{
				Denom: chain.NoahBaseDenom,
				Kind:  assettypes.AssetLockKind_ASSET_LOCK_KIND_MARKET_BASE_POOL,
			},
			expectErr: "must not use native denom anoah",
		},
		{
			name: "unspecified kind",
			lock: assettypes.AssetLock{
				Denom: "agold",
				Kind:  assettypes.AssetLockKind_ASSET_LOCK_KIND_UNSPECIFIED,
			},
			expectErr: "kind must be specified and known",
		},
		{
			name: "unknown kind",
			lock: assettypes.AssetLock{
				Denom: "agold",
				Kind:  assettypes.AssetLockKind(99),
			},
			expectErr: "kind must be specified and known",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.lock.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}
