package keeper_test

import (
	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

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

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify params
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(customParams, params)

	// Verify pool delta
	delta, err := s.keeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.Equal(customDelta))

	// Export and verify round-trip
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().True(genesis.NoahPoolDelta.Equal(exported.NoahPoolDelta))
	s.Require().Equal(genesis.Params, exported.Params)
}

func (s *KeeperTestSuite) TestDefaultGenesis() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify default params
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultParams(), params)

	// Verify zero pool delta
	delta, err := s.keeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.IsZero())
}

func (s *KeeperTestSuite) TestInitGenesis_MissingModuleAccount() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "module account has not been set")
}
