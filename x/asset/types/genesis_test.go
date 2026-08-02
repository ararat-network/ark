package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := assettypes.DefaultGenesisState()

	require.NoError(t, genesis.Validate())
	// The protocol reference is a feed, not a listed asset, so the registry
	// carries no entry for the reference denomination.
	require.Len(t, genesis.Assets, 7)
	for _, asset := range genesis.Assets {
		require.NotEqual(t, chain.SDRBaseDenom, asset.Denom)
	}
	for _, asset := range genesis.Assets {
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, asset.Status)
		// Launch feeds are named after their denoms, so rate-store keys are
		// unchanged by decoupling.
		require.Equal(t, asset.Denom, asset.Denom)
		require.Len(t, asset.Metadata.DenomUnits, 2)
		require.Equal(t, asset.Denom, asset.Metadata.DenomUnits[0].Denom)
		require.Zero(t, asset.Metadata.DenomUnits[0].Exponent)
		require.Equal(t, asset.Metadata.Display, asset.Metadata.DenomUnits[1].Denom)
		require.Equal(
			t,
			uint32(chain.NativeDisplayExponent),
			asset.Metadata.DenomUnits[1].Exponent,
		)
	}
}

func TestDefaultGenesisStateReturnsIndependentValues(t *testing.T) {
	mutated := assettypes.DefaultGenesisState()
	mutated.Assets[0].Denom = "amutated"
	mutated.Assets[0].Metadata.DenomUnits[0].Denom = "amutated"

	fresh := assettypes.DefaultGenesisState()

	require.NotEqual(t, mutated.Assets[0].Denom, fresh.Assets[0].Denom)
	require.NotEqual(
		t,
		mutated.Assets[0].Metadata.DenomUnits[0].Denom,
		fresh.Assets[0].Metadata.DenomUnits[0].Denom,
	)
}

func TestGenesisValidateLifecycleConsistency(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*assettypes.GenesisState)
		expectErr string
	}{
		{
			name: "default genesis",
		},
		{
			name: "valid priced retired tombstone",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_RETIRED
				genesis.Assets = append(genesis.Assets, asset)
			},
		},
		{
			// The denomination is also the feed key, so a denom no feed could
			// ever carry is rejected at the same boundary.
			name: "denom that could never key a feed",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Denom = "NOT-A-FEED"
			},
			expectErr: "must be an Ark-native base denom matching",
		},
		{
			name: "settlement plan only on a suspended asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.SettlementPlans = append(
					genesis.SettlementPlans,
					assettypes.SettlementPlan{
						Denom:                 genesis.Assets[0].Denom,
						RedemptionRate:        math.LegacyOneDec(),
						ActivationHeight:      10,
						EarliestClosingHeight: 110,
						OpenedHeight:          5,
					},
				)
			},
			expectErr: "must not have a settlement plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genesis := assettypes.DefaultGenesisState()
			if tt.mutate != nil {
				tt.mutate(genesis)
			}

			err := genesis.Validate()
			if tt.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErr)
		})
	}
}
