package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := assettypes.DefaultGenesisState()

	require.NoError(t, genesis.Validate())
	// The protocol reference is a feed, not a listed asset, so the registry
	// carries no entry for the reference denomination.
	require.Len(t, genesis.Assets, 10)
	for _, asset := range genesis.Assets {
		require.NotEqual(t, chain.XDRBaseDenom, asset.Denom)
	}
	for _, asset := range genesis.Assets {
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, asset.Status)
		// Each launch asset is priced by the launch feed its denomination keys.
		require.Equal(t, oracletypes.FeedPhaseActive, oracletypes.DefaultFeeds().Phase(asset.Denom))
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
						Denom:               genesis.Assets[0].Denom,
						RedemptionRate:      math.LegacyOneDec(),
						ActivationTime:      10,
						EarliestClosingTime: 110,
						OpenedHeight:        5,
						OpenedTime:          5,
					},
				)
			},
			expectErr: "must not have a settlement plan",
		},
		{
			name: "settlement plan on a suspended asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED
				genesis.SettlementPlans = []assettypes.SettlementPlan{testPlan(genesis.Assets[0].Denom)}
			},
		},
		{
			name: "repeated settlement plan",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED
				plan := testPlan(genesis.Assets[0].Denom)
				genesis.SettlementPlans = []assettypes.SettlementPlan{plan, plan}
			},
			expectErr: "genesis settlement plans must be sorted by unique denom",
		},
		{
			name: "settlement plan without an asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.SettlementPlans = []assettypes.SettlementPlan{testPlan("azzz")}
			},
			expectErr: "settlement plan azzz has no registered asset",
		},
		{
			name: "resolution records sorted by denom and version",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Version = 3
				genesis.ResolutionRecords = []assettypes.ResolutionRecord{
					testWriteOff(genesis.Assets[0].Denom, 2),
					testWriteOff(genesis.Assets[0].Denom, 3),
					testWriteOff(genesis.Assets[1].Denom, 1),
				}
			},
		},
		{
			name: "resolution records out of denom order",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.ResolutionRecords = []assettypes.ResolutionRecord{
					testWriteOff(genesis.Assets[1].Denom, 1),
					testWriteOff(genesis.Assets[0].Denom, 1),
				}
			},
			expectErr: "genesis resolution records must be sorted by unique denom and version",
		},
		{
			name: "repeated resolution record version",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Version = 3
				genesis.ResolutionRecords = []assettypes.ResolutionRecord{
					testWriteOff(genesis.Assets[0].Denom, 2),
					testWriteOff(genesis.Assets[0].Denom, 2),
				}
			},
			expectErr: "genesis resolution records must be sorted by unique denom and version",
		},
		{
			name: "resolution record without an asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.ResolutionRecords = []assettypes.ResolutionRecord{testWriteOff("azzz", 1)}
			},
			expectErr: "resolution record azzz has no registered asset",
		},
		{
			name: "resolution record ahead of its asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.ResolutionRecords = []assettypes.ResolutionRecord{
					testWriteOff(genesis.Assets[0].Denom, 2),
				}
			},
			expectErr: "version 2 exceeds asset version 1",
		},
		{
			name: "repeated emergency suspension",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.EmergencyMandate = testAppointment()
				genesis.EmergencySuspensions = []string{genesis.Assets[0].Denom, genesis.Assets[0].Denom}
			},
			expectErr: "genesis emergency suspensions must be sorted by unique denom",
		},
		{
			name: "emergency suspension without an asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.EmergencyMandate = testAppointment()
				genesis.EmergencySuspensions = []string{"azzz"}
			},
			expectErr: "emergency suspension azzz has no registered asset",
		},
		{
			name: "written-off asset with its write-off record",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
				genesis.Assets[0].Version = 2
				genesis.ResolutionRecords = []assettypes.ResolutionRecord{
					testWriteOff(genesis.Assets[0].Denom, 2),
				}
			},
		},
		{
			name: "written-off asset without a write-off record",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
			},
			expectErr: "written-off asset aaud version 1 must have a matching write-off record",
		},
		{
			// A reinstated and again written-off asset needs the later record.
			name: "written-off asset with only an earlier write-off record",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
				genesis.Assets[0].Version = 4
				genesis.ResolutionRecords = []assettypes.ResolutionRecord{
					testWriteOff(genesis.Assets[0].Denom, 2),
				}
			},
			expectErr: "written-off asset aaud version 4 must have a matching write-off record",
		},
		{
			// Only a live committee records a suspension and every replacement
			// clears them, so this pair is one no running chain can produce.
			name: "recorded suspension under a disabled mandate",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.EmergencySuspensions = []string{genesis.Assets[0].Denom}
			},
			expectErr: "disabled emergency mandate must record no suspensions",
		},
		{
			// The rule reaches no further than a disabled mandate. An expired
			// appointment still owns its term's usage, and expiry is lazy, so an
			// export taken after the window closed must round-trip.
			name: "recorded suspension under an expired appointment",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.EmergencyMandate = testAppointment()
				genesis.EmergencySuspensions = []string{genesis.Assets[0].Denom}
			},
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
