package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
)

func TestAssetIsOraclePriced(t *testing.T) {
	tests := []struct {
		name           string
		status         assettypes.AssetStatus
		oracleRequired bool
		expected       bool
	}{
		{
			name:           "active",
			status:         assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
			oracleRequired: true,
			expected:       true,
		},
		{
			name:           "retiring",
			status:         assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			oracleRequired: true,
			expected:       true,
		},
		{
			name:           "suspended",
			status:         assettypes.AssetStatus_ASSET_STATUS_SUSPENDED,
			oracleRequired: true,
		},
		{
			name:           "written off",
			status:         assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			oracleRequired: true,
		},
		{
			name:           "retired",
			status:         assettypes.AssetStatus_ASSET_STATUS_RETIRED,
			oracleRequired: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset := assettypes.DefaultGenesisState().Assets[0]
			asset.Status = test.status

			require.Equal(t, test.expected, asset.IsOraclePriced())
		})
	}
}

// derivedMetadataErr is the single failure every metadata divergence produces,
// because the rule is one comparison against the denomination's derivation
// rather than a list of per-field rules.
const derivedMetadataErr = "must equal the metadata derived from its denomination"

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
			name: "valid written-off priced asset",
			mutate: func(asset *assettypes.Asset) {
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
			},
		},
		{
			name: "invalid denom",
			mutate: func(asset *assettypes.Asset) {
				asset.Denom = "aGOLD"
			},
			expectErr: "Ark-native base denom matching",
		},
		{
			name: "native denom",
			mutate: func(asset *assettypes.Asset) {
				asset.Denom = chain.NoahBaseDenom
			},
			expectErr: "is the numeraire and is never priced",
		},
		// Metadata is derived from the denomination, so every divergence from
		// that derivation is the same failure however it is reached: a base
		// naming another asset, a unit list that is missing, reordered, or
		// padded, or a description a genesis author wrote by hand.
		{
			name: "metadata base mismatch",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Base = "asilver"
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "nil denomination unit",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits[1] = nil
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "missing display denomination unit",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits = asset.Metadata.DenomUnits[:1]
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "nonstandard display denom",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Display = "gold-token"
				asset.Metadata.DenomUnits[1].Denom = "gold-token"
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "nonstandard display exponent",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits[1].Exponent = 6
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "intermediate denomination unit",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits = []*banktypes.DenomUnit{
					asset.Metadata.DenomUnits[0],
					{Denom: "milligold", Exponent: 15},
					asset.Metadata.DenomUnits[1],
				}
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "denomination alias",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.DenomUnits[0].Aliases = []string{"attogold"}
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "rewritten description",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Description = "Backed by something else entirely."
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "rewritten symbol",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Symbol = "GOLD"
			},
			expectErr: derivedMetadataErr,
		},
		{
			name: "rewritten name",
			mutate: func(asset *assettypes.Asset) {
				asset.Metadata.Name = "Gold Ark"
			},
			expectErr: derivedMetadataErr,
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
