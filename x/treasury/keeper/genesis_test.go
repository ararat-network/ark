package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "noah/pkg/chain"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestInitGenesis() {
	s.accountKeeper.EXPECT().
		GetModuleAccount(s.ctx, types.ModuleName).
		Return(authtypes.NewEmptyModuleAccount(types.ModuleName))

	customTaxRate := math.LegacyNewDecWithPrec(5, 3)       // 0.5%
	customRewardWeight := math.LegacyNewDecWithPrec(10, 2) // 10%
	customTaxCaps := []types.TaxCap{
		{Denom: "ukrw", TaxCap: math.NewInt(1300000000)},
		{Denom: "uusd", TaxCap: math.NewInt(1000000)},
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
		sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(500000))),
		customEpochStates,
	)

	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify tax rate
	taxRate, err := s.keeper.TaxRate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(customTaxRate.Equal(taxRate))

	// Verify reward weight
	rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(customRewardWeight.Equal(rewardWeight))

	// Verify tax caps
	cap, err := s.keeper.TaxCaps.Get(s.ctx, "uusd")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1000000), cap)

	cap, err = s.keeper.TaxCaps.Get(s.ctx, "ukrw")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1300000000), cap)

	// Verify epoch tax proceeds
	proceeds, err := s.keeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(5000), proceeds.TaxProceeds.AmountOf("uusd"))

	// Verify epoch initial issuance
	issuance, err := s.keeper.EpochInitialIssuance.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(500000), issuance.Issuance.AmountOf(chain.MicroArkDenom))

	// Verify epoch states
	epochState, err := s.keeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDec(100).Equal(epochState.TaxReward))
	s.Require().True(math.LegacyNewDec(200).Equal(epochState.SeigniorageReward))
	s.Require().Equal(math.NewInt(1000000), epochState.TotalStakedArk)
}

func (s *KeeperTestSuite) TestInitGenesis_MissingModuleAccount() {
	s.accountKeeper.EXPECT().
		GetModuleAccount(s.ctx, types.ModuleName).
		Return(nil)

	// When EpochInitialIssuance is empty, RecordEpochInitialIssuance is called
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return(nil, nil)
	s.bankKeeper.EXPECT().GetSupply(s.ctx, chain.MicroArkDenom).
		Return(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(1000000)))

	genesis := types.DefaultGenesisState()
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "module account has not been set")
}

func (s *KeeperTestSuite) TestExportGenesis() {
	s.accountKeeper.EXPECT().
		GetModuleAccount(s.ctx, types.ModuleName).
		Return(authtypes.NewEmptyModuleAccount(types.ModuleName))

	customTaxRate := math.LegacyNewDecWithPrec(5, 3)       // 0.5%
	customRewardWeight := math.LegacyNewDecWithPrec(10, 2) // 10%
	customTaxCaps := []types.TaxCap{
		{Denom: "ukrw", TaxCap: math.NewInt(1300000000)},
		{Denom: "uusd", TaxCap: math.NewInt(1000000)},
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
		sdk.NewCoins(sdk.NewCoin(chain.MicroArkDenom, math.NewInt(500000))),
		customEpochStates,
	)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Export at the last block of epoch 0 so the current epoch state is included.
	s.setBlockHeight(int64(chain.BlocksPerWeek) - 1)
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NotNil(exported)

	// Verify round-trip of scalar values
	s.Require().True(genesis.TaxRate.Equal(exported.TaxRate))
	s.Require().True(genesis.RewardWeight.Equal(exported.RewardWeight))
	s.Require().Equal(genesis.Params, exported.Params)

	// Verify tax caps round-trip
	s.Require().Len(exported.TaxCaps, 2)
	s.Require().Equal(genesis.TaxCaps[0].Denom, exported.TaxCaps[0].Denom)
	s.Require().Equal(genesis.TaxCaps[0].TaxCap, exported.TaxCaps[0].TaxCap)
	s.Require().Equal(genesis.TaxCaps[1].Denom, exported.TaxCaps[1].Denom)
	s.Require().Equal(genesis.TaxCaps[1].TaxCap, exported.TaxCaps[1].TaxCap)

	// Verify epoch tax proceeds
	s.Require().NoError(err)
	s.Require().Equal(genesis.EpochTaxProceeds.AmountOf("uusd"), exported.EpochTaxProceeds.AmountOf("uusd"))

	// Verify epoch initial issuance
	s.Require().NoError(err)
	s.Require().Equal(genesis.EpochInitialIssuance.AmountOf(chain.MicroArkDenom), exported.EpochInitialIssuance.AmountOf(chain.MicroArkDenom))

	// Verify epoch states
	s.Require().NoError(err)
	s.Require().True(genesis.EpochStates[0].TaxReward.Equal(exported.EpochStates[0].TaxReward))
	s.Require().True(genesis.EpochStates[0].SeigniorageReward.Equal(exported.EpochStates[0].SeigniorageReward))
	s.Require().True(genesis.EpochStates[0].TotalStakedArk.Equal(exported.EpochStates[0].TotalStakedArk))
}
