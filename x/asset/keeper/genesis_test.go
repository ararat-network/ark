package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestInitExportGenesis() {
	genesis := fullGenesisState()
	s.Require().NoError(genesis.Validate())
	for _, asset := range genesis.Assets {
		s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, asset.Metadata)
	}

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis, exported)
	s.Require().NoError(exported.Validate())
}

// TestInitGenesisRequiresActiveFeeds covers the one asset genesis rule that
// cannot live in GenesisState.Validate: the feed registry is x/oracle state, so
// feed existence is checked at the keeper boundary.
func (s *KeeperTestSuite) TestInitGenesisRequiresActiveFeeds() {
	genesis := types.DefaultGenesisState()
	s.feedPhases[genesis.Assets[0].Denom] = oracletypes.FeedPhaseOff
	for _, asset := range genesis.Assets {
		s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, asset.Metadata).AnyTimes()
	}

	err := s.keeper.InitGenesis(s.ctx, genesis)

	s.Require().ErrorIs(err, types.ErrAssetNotPriceable)
}

// TestInitGenesisRejectsAddingFeedForLiveAsset holds import to the one runtime
// admission rule. A launching chain lists its live feeds in the active set
// outright — only runtime transitions pass through Adding — so import reaches
// the rule with nothing to wait for.
func (s *KeeperTestSuite) TestInitGenesisRejectsAddingFeedForLiveAsset() {
	genesis := types.DefaultGenesisState()
	s.Require().True(genesis.Assets[0].IsOraclePriced())
	s.feedPhases[genesis.Assets[0].Denom] = oracletypes.FeedPhaseAdding
	for _, asset := range genesis.Assets {
		s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, asset.Metadata).AnyTimes()
	}

	err := s.keeper.InitGenesis(s.ctx, genesis)

	s.Require().ErrorIs(err, types.ErrAssetNotPriceable)
}

// TestInitGenesisAllowsMissingFeedsForDeadAssets is the other half of that
// rule: a suspended, written-off, or retired asset may legitimately name a
// feed governance has since removed, so its absence must not block import.
func (s *KeeperTestSuite) TestInitGenesisAllowsMissingFeedsForDeadAssets() {
	genesis := types.DefaultGenesisState()
	genesis.Assets[0].Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	s.feedPhases[genesis.Assets[0].Denom] = oracletypes.FeedPhaseOff
	s.Require().NoError(genesis.Validate())
	for _, asset := range genesis.Assets {
		s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, asset.Metadata)
	}
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, gomock.Any()).
		Return(sdk.NewCoin(genesis.Assets[0].Denom, math.ZeroInt())).
		AnyTimes()

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
}

func (s *KeeperTestSuite) TestInitGenesisRejectsInvalidStateBeforeWrites() {
	tests := []struct {
		name      string
		genesis   func() *types.GenesisState
		expectErr string
	}{
		{
			name:      "nil genesis",
			genesis:   func() *types.GenesisState { return nil },
			expectErr: "asset genesis state is nil",
		},
		{
			name: "invalid genesis",
			genesis: func() *types.GenesisState {
				genesis := types.DefaultGenesisState()
				genesis.Assets[0], genesis.Assets[1] = genesis.Assets[1], genesis.Assets[0]
				return genesis
			},
			expectErr: "invalid asset genesis state: genesis assets must be sorted by unique denom",
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			err := s.keeper.InitGenesis(s.ctx, test.genesis())
			s.Require().ErrorContains(err, test.expectErr)

			hasAsset, hasErr := s.keeper.Assets.Has(s.ctx, chain.CNYBaseDenom)
			s.Require().NoError(hasErr)
			s.Require().False(hasAsset)
		})
	}
}

func (s *KeeperTestSuite) TestExportGenesisUsesCollectionKeyOrder() {
	assets := types.DefaultGenesisState().Assets
	for _, asset := range []types.Asset{assets[1], assets[0]} {
		s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	}
	s.Require().NoError(s.keeper.EmergencyMandate.Set(
		s.ctx,
		types.DefaultEmergencyMandate(),
	))

	plans := []types.SettlementPlan{
		{
			Denom:                 assets[1].Denom,
			RedemptionRate:        math.LegacyOneDec(),
			ActivationHeight:      10,
			EarliestClosingHeight: 110,
			OpenedHeight:          1,
		},
		{
			Denom:            assets[0].Denom,
			RedemptionRate:   math.LegacyOneDec(),
			ActivationHeight: 10,
		},
	}
	for _, plan := range plans {
		s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, plan.Denom, plan))
	}

	records := []types.ResolutionRecord{
		{
			Denom:             assets[1].Denom,
			Kind:              types.ResolutionKind_RESOLUTION_KIND_WRITE_OFF,
			Version:           3,
			ResolutionHeight:  20,
			OutstandingSupply: sdk.NewInt64Coin(assets[1].Denom, 100),
		},
		{
			Denom:             assets[0].Denom,
			Version:           4,
			ResolutionHeight:  30,
			OutstandingSupply: sdk.NewInt64Coin(assets[0].Denom, 100),
		},
		{
			Denom:             assets[0].Denom,
			Version:           3,
			ResolutionHeight:  20,
			OutstandingSupply: sdk.NewInt64Coin(assets[0].Denom, 100),
		},
	}
	for _, record := range records {
		s.Require().NoError(s.keeper.ResolutionRecords.Set(
			s.ctx,
			record.Key(),
			record,
		))
	}

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(
		[]string{assets[0].Denom, assets[1].Denom},
		[]string{exported.Assets[0].Denom, exported.Assets[1].Denom},
	)
	s.Require().Equal(
		[]string{assets[0].Denom, assets[1].Denom},
		[]string{
			exported.SettlementPlans[0].Denom,
			exported.SettlementPlans[1].Denom,
		},
	)
	s.Require().Equal(
		[]struct {
			denom   string
			version uint64
		}{
			{denom: assets[0].Denom, version: 3},
			{denom: assets[0].Denom, version: 4},
			{denom: assets[1].Denom, version: 3},
		},
		[]struct {
			denom   string
			version uint64
		}{
			{
				denom:   exported.ResolutionRecords[0].Denom,
				version: exported.ResolutionRecords[0].Version,
			},
			{
				denom:   exported.ResolutionRecords[1].Denom,
				version: exported.ResolutionRecords[1].Version,
			},
			{
				denom:   exported.ResolutionRecords[2].Denom,
				version: exported.ResolutionRecords[2].Version,
			},
		},
	)
}

func fullGenesisState() *types.GenesisState {
	genesis := types.DefaultGenesisState()

	genesis.Assets[0].Status = types.AssetStatus_ASSET_STATUS_WRITTEN_OFF
	genesis.Assets[0].Version = 3
	genesis.Assets[1].Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	genesis.Assets[1].Version = 2
	genesis.SettlementPlans = []types.SettlementPlan{
		{
			Denom:                 genesis.Assets[1].Denom,
			RedemptionRate:        math.LegacyOneDec(),
			ActivationHeight:      10,
			EarliestClosingHeight: 110,
			OpenedHeight:          1,
		},
	}
	genesis.ResolutionRecords = []types.ResolutionRecord{
		{
			Denom:             genesis.Assets[0].Denom,
			Kind:              types.ResolutionKind_RESOLUTION_KIND_WRITE_OFF,
			Version:           genesis.Assets[0].Version,
			ResolutionHeight:  20,
			OutstandingSupply: sdk.NewInt64Coin(genesis.Assets[0].Denom, 100),
		},
	}
	genesis.EmergencySuspensions = []string{}

	return genesis
}

func (s *KeeperTestSuite) TestInitGenesisEnforcesSupplyInvariants() {
	// Residual supply on a RETIRED tombstone must be disclosed by a record.
	retiredGenesis := types.DefaultGenesisState()
	retiredGenesis.Assets[0].Status = types.AssetStatus_ASSET_STATUS_RETIRED
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, retiredGenesis.Assets[0].Denom).
		Return(sdk.NewInt64Coin(retiredGenesis.Assets[0].Denom, 5))
	err := s.keeper.InitGenesis(s.ctx, retiredGenesis)
	s.Require().ErrorContains(err, "without a resolution record")

	// The same tombstone with a matching record imports cleanly.
	retiredGenesis.ResolutionRecords = []types.ResolutionRecord{
		{
			Denom:            retiredGenesis.Assets[0].Denom,
			Kind:             types.ResolutionKind_RESOLUTION_KIND_RETIREMENT_RESIDUAL,
			Version:          retiredGenesis.Assets[0].Version,
			ResolutionHeight: 20,
			OutstandingSupply: sdk.NewInt64Coin(
				retiredGenesis.Assets[0].Denom,
				5,
			),
		},
	}
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, retiredGenesis.Assets[0].Denom).
		Return(sdk.NewInt64Coin(retiredGenesis.Assets[0].Denom, 5))
	s.bankKeeper.EXPECT().SetDenomMetaData(s.ctx, gomock.Any()).AnyTimes()
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, retiredGenesis))
}

func (s *KeeperTestSuite) TestInitGenesisRejectsAuthorityEmergencyCommittee() {
	genesis := types.DefaultGenesisState()
	genesis.EmergencyMandate = types.EmergencyMandate{
		Envelope: mandate.Envelope{
			Term:             1,
			Committee:        authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			ActivationHeight: 1,
			ExpiryHeight:     100,
		},
	}
	s.Require().NoError(genesis.Validate())

	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "distinct from the Asset authority")
}

// TestGenesisRoundTripsParams pins params through both halves of the genesis
// path. A silent drop here would restart a chain on the launch delay rather
// than the one governance last voted for.
func (s *KeeperTestSuite) TestGenesisRoundTripsParams() {
	genesis := types.DefaultGenesisState()
	genesis.Params.SettlementActivationDelayBlocks = 4_321
	genesis.Assets = nil

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(uint64(4_321), stored.SettlementActivationDelayBlocks)

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.Params, exported.Params)
}

// TestInitGenesisRejectsInvalidParams keeps a launch genesis from starting a
// chain with no settlement correction window at all.
func (s *KeeperTestSuite) TestInitGenesisRejectsInvalidParams() {
	genesis := types.DefaultGenesisState()
	genesis.Params.SettlementActivationDelayBlocks = 0

	s.Require().ErrorContains(
		s.keeper.InitGenesis(s.ctx, genesis),
		"SettlementActivationDelayBlocks must be between one and",
	)
}
