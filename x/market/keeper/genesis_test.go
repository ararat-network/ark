package keeper_test

import (
	"errors"

	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestInitExportGenesis() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, chain.SDRBaseDenom).Return(math.LegacyZeroDec(), nil)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify params
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.Params, params)

	// Verify pool delta
	delta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.Equal(genesis.ArkPoolDelta))

	// Export and verify round-trip
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().True(genesis.ArkPoolDelta.Equal(exported.ArkPoolDelta))
	s.Require().Equal(genesis.Params, exported.Params)
}

func (s *KeeperTestSuite) TestInitGenesis_MissingModuleAccount() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "module account has not been set")
}

func (s *KeeperTestSuite) TestInitGenesis_UnknownBasePoolDenom() {
	genesis := types.DefaultGenesisState()
	genesis.Params.BasePool.Denom = "afoo"
	genesis.ArkPoolDelta = math.LegacyOneDec()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, "afoo").Return(math.LegacyZeroDec(), oracletypes.ErrUnknownDenom)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorIs(err, oracletypes.ErrUnknownDenom)
	s.Require().ErrorContains(err, "base pool denom afoo is not configured in oracle")

	params, getErr := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().Equal(types.DefaultParams(), params)
	delta, getErr := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().True(delta.IsZero())
}

func (s *KeeperTestSuite) TestInitGenesis_OracleLookupFailure() {
	genesis := types.DefaultGenesisState()
	genesis.ArkPoolDelta = math.LegacyOneDec()
	oracleErr := errors.New("oracle params unavailable")

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, chain.SDRBaseDenom).Return(math.LegacyZeroDec(), oracleErr)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorIs(err, oracleErr)
	s.Require().ErrorContains(err, "checking base pool denom asdr in oracle")

	params, getErr := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().Equal(types.DefaultParams(), params)
	delta, getErr := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(getErr)
	s.Require().True(delta.IsZero())
}
