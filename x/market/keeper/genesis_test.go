package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"noah/x/market/types"
)

func (s *KeeperTestSuite) TestInitExportGenesis() {
	// Custom genesis with non-zero pool delta
	customParams := types.Params{
		BasePool:           math.LegacyNewDec(2000000000000),
		PoolRecoveryPeriod: 28800,
		MinStabilitySpread: math.LegacyNewDecWithPrec(5, 2),
	}
	customDelta := math.LegacyNewDec(12345)
	genesis := types.NewGenesisState(customDelta, customParams)

	s.accountKeeper.EXPECT().GetModuleAccount(gomock.Any(), types.ModuleName).Return(nil).Times(1)
	err := s.marketKeeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify params
	params, err := s.marketKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(customParams, params)

	// Verify pool delta
	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.Equal(customDelta))

	// Export and verify round-trip
	exported, err := s.marketKeeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().True(genesis.NoahPoolDelta.Equal(exported.NoahPoolDelta))
	s.Require().Equal(genesis.Params, exported.Params)
}

func (s *KeeperTestSuite) TestDefaultGenesis() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(gomock.Any(), types.ModuleName).Return(nil).Times(1)
	err := s.marketKeeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify default params
	params, err := s.marketKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultParams(), params)

	// Verify zero pool delta
	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.IsZero())
}
