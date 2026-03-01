package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestInitGenesis_Default() {
	s.accountKeeper.EXPECT().
		GetModuleAccount(s.ctx, types.ModuleName).
		Return(authtypes.NewEmptyModuleAccount(types.ModuleName))

	genesis := types.DefaultGenesisState()
	// When EpochInitialIssuance is empty, RecordEpochInitialIssuance is called
	s.oracleKeeper.EXPECT().Whitelist(s.ctx).Return(nil)
	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000)))

	err := s.treasuryKeeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify params stored
	params, err := s.treasuryKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultParams(), params)

	// Verify tax rate stored
	taxRate, err := s.treasuryKeeper.TaxRate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(types.DefaultTaxRate.Equal(taxRate))

	// Verify reward weight stored
	rewardWeight, err := s.treasuryKeeper.RewardWeight.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(types.DefaultRewardWeight.Equal(rewardWeight))
}

func (s *KeeperTestSuite) TestInitGenesis_Custom() {
	s.accountKeeper.EXPECT().
		GetModuleAccount(s.ctx, types.ModuleName).
		Return(authtypes.NewEmptyModuleAccount(types.ModuleName))

	customTaxRate := math.LegacyNewDecWithPrec(5, 3) // 0.5%
	customRewardWeight := math.LegacyNewDecWithPrec(10, 2) // 10%
	customTaxCaps := []types.TaxCap{
		{Denom: "uusd", TaxCap: math.NewInt(1000000)},
		{Denom: "ukrw", TaxCap: math.NewInt(1300000000)},
	}
	customEpochStates := []types.EpochState{
		{
			Epoch:             0,
			TaxReward:         math.LegacyNewDec(100),
			SeigniorageReward: math.LegacyNewDec(200),
			TotalStakedArk:    math.NewInt(1000000),
		},
	}

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		customTaxRate,
		customRewardWeight,
		customTaxCaps,
		sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(5000))),
		sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(500000))),
		customEpochStates,
	)

	err := s.treasuryKeeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify tax rate
	taxRate, err := s.treasuryKeeper.TaxRate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(customTaxRate.Equal(taxRate))

	// Verify reward weight
	rewardWeight, err := s.treasuryKeeper.RewardWeight.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(customRewardWeight.Equal(rewardWeight))

	// Verify tax caps
	cap, err := s.treasuryKeeper.TaxCaps.Get(s.ctx, "uusd")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1000000), cap)

	cap, err = s.treasuryKeeper.TaxCaps.Get(s.ctx, "ukrw")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1300000000), cap)

	// Verify epoch tax proceeds
	proceeds, err := s.treasuryKeeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(5000), proceeds.TaxProceeds.AmountOf("uusd"))

	// Verify epoch initial issuance
	issuance, err := s.treasuryKeeper.EpochInitialIssuance.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(500000), issuance.Issuance.AmountOf(core.MicroArkDenom))

	// Verify epoch states
	epochState, err := s.treasuryKeeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDec(100).Equal(epochState.TaxReward))
	s.Require().True(math.LegacyNewDec(200).Equal(epochState.SeigniorageReward))
	s.Require().Equal(math.NewInt(1000000), epochState.TotalStakedArk)
}

func (s *KeeperTestSuite) TestExportGenesis() {
	s.accountKeeper.EXPECT().
		GetModuleAccount(s.ctx, types.ModuleName).
		Return(authtypes.NewEmptyModuleAccount(types.ModuleName))

	customTaxRate := math.LegacyNewDecWithPrec(5, 3)
	customRewardWeight := math.LegacyNewDecWithPrec(10, 2)

	genesis := types.NewGenesisState(
		types.DefaultParams(),
		customTaxRate,
		customRewardWeight,
		[]types.TaxCap{{Denom: "uusd", TaxCap: math.NewInt(1000000)}},
		sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(5000))),
		sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(500000))),
		[]types.EpochState{},
	)

	err := s.treasuryKeeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Export at block 0 (epoch 0, not last block) — no epoch states expected
	exported, err := s.treasuryKeeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NotNil(exported)

	// Verify round-trip of scalar values
	s.Require().True(customTaxRate.Equal(exported.TaxRate))
	s.Require().True(customRewardWeight.Equal(exported.RewardWeight))
	s.Require().Equal(types.DefaultParams(), exported.Params)

	// Verify tax caps round-trip
	s.Require().Len(exported.TaxCaps, 1)
	s.Require().Equal("uusd", exported.TaxCaps[0].Denom)
	s.Require().Equal(math.NewInt(1000000), exported.TaxCaps[0].TaxCap)

	// Verify tax proceeds round-trip
	s.Require().Equal(math.NewInt(5000), exported.EpochTaxProceeds.AmountOf("uusd"))

	// Verify epoch initial issuance round-trip
	s.Require().Equal(math.NewInt(500000), exported.EpochInitialIssuance.AmountOf(core.MicroArkDenom))
}

func (s *KeeperTestSuite) TestInitGenesis_MissingModuleAccount() {
	s.accountKeeper.EXPECT().
		GetModuleAccount(s.ctx, types.ModuleName).
		Return(nil)

	// When EpochInitialIssuance is empty, RecordEpochInitialIssuance is called
	s.oracleKeeper.EXPECT().Whitelist(s.ctx).Return(nil)
	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000)))

	genesis := types.DefaultGenesisState()
	err := s.treasuryKeeper.InitGenesis(s.ctx, genesis)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "module account has not been set")
}
