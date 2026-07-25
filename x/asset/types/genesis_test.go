package types_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"ark/pkg/chain"
	assettypes "ark/x/asset/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := assettypes.DefaultGenesisState()

	require.NoError(t, genesis.Validate())
	require.Len(t, genesis.Assets, 8)
	require.Len(t, genesis.OracleTargets.Denoms, 8)
	require.Equal(t, assettypes.InitialOracleTargetVersion, genesis.OracleTargets.Version)
	for i, asset := range genesis.Assets {
		require.Equal(t, asset.Denom, genesis.OracleTargets.Denoms[i])
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, asset.Status)
		require.True(t, asset.OracleRequired)
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
	mutated.OracleTargets.Denoms[0] = "amutated"

	fresh := assettypes.DefaultGenesisState()

	require.NotEqual(t, mutated.Assets[0].Denom, fresh.Assets[0].Denom)
	require.NotEqual(
		t,
		mutated.Assets[0].Metadata.DenomUnits[0].Denom,
		fresh.Assets[0].Metadata.DenomUnits[0].Denom,
	)
	require.NotEqual(t, mutated.OracleTargets.Denoms[0], fresh.OracleTargets.Denoms[0])
}

func TestGenesisValidateLifecycleTargetConsistency(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*assettypes.GenesisState)
		expectErr string
	}{
		{
			name: "default genesis",
		},
		{
			name: "valid unpriced active asset",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.OracleRequired = false
				genesis.Assets = append(genesis.Assets, asset)
			},
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
			name: "valid pending target addition",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_PENDING
				genesis.Assets = append(genesis.Assets, asset)
				pending := append(slices.Clone(genesis.OracleTargets.Denoms), asset.Denom)
				genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               pending,
					Version:              genesis.OracleTargets.Version + 1,
					ActivationVoteHeight: 10,
				}
			},
		},
		{
			name: "valid pending target removal",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status =
					assettypes.AssetStatus_ASSET_STATUS_REMOVAL_PENDING
				genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               slices.Clone(genesis.OracleTargets.Denoms[1:]),
					Version:              genesis.OracleTargets.Version + 1,
					ActivationVoteHeight: 10,
				}
			},
		},
		{
			name: "valid delisting target removal",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status = assettypes.AssetStatus_ASSET_STATUS_DELISTING
				genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               slices.Clone(genesis.OracleTargets.Denoms[1:]),
					Version:              genesis.OracleTargets.Version + 1,
					ActivationVoteHeight: 10,
				}
			},
		},
		{
			name: "valid delisted asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status = assettypes.AssetStatus_ASSET_STATUS_DELISTED
				genesis.OracleTargets.Denoms =
					slices.Clone(genesis.OracleTargets.Denoms[1:])
			},
		},
		{
			name: "valid settling asset",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_SETTLING
				asset.Version = 2
				genesis.Assets = append(genesis.Assets, asset)
				plan := testSettlementPlan()
				plan.Denom = asset.Denom
				genesis.SettlementPlans = append(genesis.SettlementPlans, plan)
			},
		},
		{
			name: "valid relisting target addition",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_RELISTING
				asset.Version = 2
				genesis.Assets = append(genesis.Assets, asset)
				pending := append(slices.Clone(genesis.OracleTargets.Denoms), asset.Denom)
				genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               pending,
					Version:              genesis.OracleTargets.Version + 1,
					ActivationVoteHeight: 10,
				}
			},
		},
		{
			name: "valid relisting asset with settlement",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_RELISTING
				asset.Version = 2
				genesis.Assets = append(genesis.Assets, asset)
				pending := append(slices.Clone(genesis.OracleTargets.Denoms), asset.Denom)
				genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               pending,
					Version:              genesis.OracleTargets.Version + 1,
					ActivationVoteHeight: 10,
				}
				plan := testSettlementPlan()
				plan.Denom = asset.Denom
				plan.EarliestClosingHeight = 20
				genesis.SettlementPlans = append(genesis.SettlementPlans, plan)
			},
		},
		{
			name: "valid relisting asset after target activation",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_RELISTING
				asset.Version = 2
				genesis.Assets = append(genesis.Assets, asset)
				genesis.OracleTargets.Denoms =
					append(genesis.OracleTargets.Denoms, asset.Denom)
			},
		},
		{
			name: "valid written-off asset and history",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
				asset.Version = 3
				genesis.Assets = append(genesis.Assets, asset)
				record := testWriteOffRecord()
				record.Denom = asset.Denom
				record.OutstandingSupply.Denom = asset.Denom
				genesis.WriteOffRecords = append(genesis.WriteOffRecords, record)
			},
		},
		{
			name: "valid reinstated asset retains write-off history",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_DELISTED
				asset.Version = 4
				genesis.Assets = append(genesis.Assets, asset)
				record := testWriteOffRecord()
				record.Denom = asset.Denom
				record.OutstandingSupply.Denom = asset.Denom
				plan := testSettlementPlan()
				plan.Denom = asset.Denom
				record.SettlementPlan = &plan
				genesis.WriteOffRecords = append(genesis.WriteOffRecords, record)
			},
		},
		{
			name: "assets not sorted",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0], genesis.Assets[1] =
					genesis.Assets[1], genesis.Assets[0]
			},
			expectErr: "assets must be sorted by unique denom",
		},
		{
			name: "duplicate asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets = append(genesis.Assets, genesis.Assets[len(genesis.Assets)-1])
			},
			expectErr: "assets must be sorted by unique denom",
		},
		{
			name: "active target has no asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.OracleTargets.Denoms =
					append(genesis.OracleTargets.Denoms, "azzz")
			},
			expectErr: "active vote target azzz has no registered asset",
		},
		{
			name: "unpriced asset is targeted",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].OracleRequired = false
			},
			expectErr: "without an Oracle requirement",
		},
		{
			name: "pending asset remains active target",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status =
					assettypes.AssetStatus_ASSET_STATUS_PENDING
			},
			expectErr: "pending asset",
		},
		{
			name: "priced active asset missing target",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.OracleTargets.Denoms =
					slices.Clone(genesis.OracleTargets.Denoms[1:])
			},
			expectErr: "must remain in active and staged vote targets",
		},
		{
			name: "retired asset remains targeted",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status =
					assettypes.AssetStatus_ASSET_STATUS_RETIRED
			},
			expectErr: "retired asset",
		},
		{
			name: "removal pending without target transition",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status =
					assettypes.AssetStatus_ASSET_STATUS_REMOVAL_PENDING
			},
			expectErr: "must be active and absent from pending vote targets",
		},
		{
			name: "delisting without target transition",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status =
					assettypes.AssetStatus_ASSET_STATUS_DELISTING
			},
			expectErr: "delisting asset",
		},
		{
			name: "delisted asset remains targeted",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.Assets[0].Status =
					assettypes.AssetStatus_ASSET_STATUS_DELISTED
			},
			expectErr: "delisted asset",
		},
		{
			name: "settling asset missing settlement plan",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_SETTLING
				genesis.Assets = append(genesis.Assets, asset)
			},
			expectErr: "must have a settlement plan",
		},
		{
			name: "active asset has settlement plan",
			mutate: func(genesis *assettypes.GenesisState) {
				plan := testSettlementPlan()
				plan.Denom = genesis.Assets[0].Denom
				plan.Version = genesis.Assets[0].Version
				genesis.SettlementPlans = append(genesis.SettlementPlans, plan)
			},
			expectErr: "must not have a settlement plan",
		},
		{
			name: "settlement plan has no asset",
			mutate: func(genesis *assettypes.GenesisState) {
				plan := testSettlementPlan()
				plan.Denom = "azzz"
				genesis.SettlementPlans = append(genesis.SettlementPlans, plan)
			},
			expectErr: "settlement plan azzz has no registered asset",
		},
		{
			name: "settlement plan version differs from asset",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_SETTLING
				genesis.Assets = append(genesis.Assets, asset)
				plan := testSettlementPlan()
				plan.Denom = asset.Denom
				genesis.SettlementPlans = append(genesis.SettlementPlans, plan)
			},
			expectErr: "must match asset version",
		},
		{
			name: "settlement plans not sorted",
			mutate: func(genesis *assettypes.GenesisState) {
				first := testAsset("ayyy")
				first.Status = assettypes.AssetStatus_ASSET_STATUS_SETTLING
				first.Version = 2
				second := testAsset("azzz")
				second.Status = assettypes.AssetStatus_ASSET_STATUS_SETTLING
				second.Version = 2
				genesis.Assets = append(genesis.Assets, first, second)

				firstPlan := testSettlementPlan()
				firstPlan.Denom = first.Denom
				secondPlan := testSettlementPlan()
				secondPlan.Denom = second.Denom
				genesis.SettlementPlans = append(
					genesis.SettlementPlans,
					secondPlan,
					firstPlan,
				)
			},
			expectErr: "settlement plans must be sorted by unique denom",
		},
		{
			name: "relisting settlement missing earliest closing height",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_RELISTING
				asset.Version = 2
				genesis.Assets = append(genesis.Assets, asset)
				pending := append(slices.Clone(genesis.OracleTargets.Denoms), asset.Denom)
				genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               pending,
					Version:              genesis.OracleTargets.Version + 1,
					ActivationVoteHeight: 10,
				}
				plan := testSettlementPlan()
				plan.Denom = asset.Denom
				genesis.SettlementPlans = append(genesis.SettlementPlans, plan)
			},
			expectErr: "settlement plan must have an earliest closing height",
		},
		{
			name: "written-off asset missing current record",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				asset.Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
				asset.Version = 3
				genesis.Assets = append(genesis.Assets, asset)
			},
			expectErr: "must have a matching write-off record",
		},
		{
			name: "write-off record has no asset",
			mutate: func(genesis *assettypes.GenesisState) {
				genesis.WriteOffRecords = append(
					genesis.WriteOffRecords,
					testWriteOffRecord(),
				)
			},
			expectErr: "write-off record agold has no registered asset",
		},
		{
			name: "write-off record version exceeds asset",
			mutate: func(genesis *assettypes.GenesisState) {
				asset := testAsset("azzz")
				genesis.Assets = append(genesis.Assets, asset)
				record := testWriteOffRecord()
				record.Denom = asset.Denom
				record.OutstandingSupply.Denom = asset.Denom
				genesis.WriteOffRecords = append(genesis.WriteOffRecords, record)
			},
			expectErr: "exceeds asset version",
		},
		{
			name: "write-off records not sorted",
			mutate: func(genesis *assettypes.GenesisState) {
				first := testAsset("ayyy")
				first.Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
				first.Version = 3
				second := testAsset("azzz")
				second.Status = assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF
				second.Version = 3
				genesis.Assets = append(genesis.Assets, first, second)

				firstRecord := testWriteOffRecord()
				firstRecord.Denom = first.Denom
				firstRecord.OutstandingSupply.Denom = first.Denom
				secondRecord := testWriteOffRecord()
				secondRecord.Denom = second.Denom
				secondRecord.OutstandingSupply.Denom = second.Denom
				genesis.WriteOffRecords = append(
					genesis.WriteOffRecords,
					secondRecord,
					firstRecord,
				)
			},
			expectErr: "write-off records must be sorted by unique denom and version",
		},
		{
			name: "pending target has no asset",
			mutate: func(genesis *assettypes.GenesisState) {
				pending := append(slices.Clone(genesis.OracleTargets.Denoms), "azzz")
				genesis.OracleTargets.Pending = &assettypes.PendingOracleTargets{
					Denoms:               pending,
					Version:              genesis.OracleTargets.Version + 1,
					ActivationVoteHeight: 10,
				}
			},
			expectErr: "pending vote target azzz has no registered asset",
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
