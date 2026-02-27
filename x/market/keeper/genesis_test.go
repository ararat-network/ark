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
	s.marketKeeper.InitGenesis(s.ctx, genesis)

	// Verify params
	params, err := s.marketKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(customParams.BasePool.Equal(params.BasePool))
	s.Require().Equal(customParams.PoolRecoveryPeriod, params.PoolRecoveryPeriod)
	s.Require().True(customParams.MinStabilitySpread.Equal(params.MinStabilitySpread))

	// Verify pool delta
	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.Equal(customDelta))

	// Export and verify round-trip
	exported := s.marketKeeper.ExportGenesis(s.ctx)
	s.Require().True(genesis.NoahPoolDelta.Equal(exported.NoahPoolDelta))
	s.Require().True(genesis.Params.BasePool.Equal(exported.Params.BasePool))
	s.Require().Equal(genesis.Params.PoolRecoveryPeriod, exported.Params.PoolRecoveryPeriod)
	s.Require().True(genesis.Params.MinStabilitySpread.Equal(exported.Params.MinStabilitySpread))
}

func (s *KeeperTestSuite) TestDefaultGenesis() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(gomock.Any(), types.ModuleName).Return(nil).Times(1)
	s.marketKeeper.InitGenesis(s.ctx, genesis)

	// Verify default params
	params, err := s.marketKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(types.DefaultParams().BasePool.Equal(params.BasePool))
	s.Require().Equal(types.DefaultParams().PoolRecoveryPeriod, params.PoolRecoveryPeriod)
	s.Require().True(types.DefaultParams().MinStabilitySpread.Equal(params.MinStabilitySpread))

	// Verify zero pool delta
	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.IsZero())
}
