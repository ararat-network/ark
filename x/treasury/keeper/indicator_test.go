package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestGetEpoch() {
	tests := []struct {
		name        string
		blockHeight int64
		expected    uint64
	}{
		{"block 0 — epoch 0", 0, 0},
		{"last block of epoch 0", int64(core.BlocksPerWeek) - 1, 0},
		{"first block of epoch 1", int64(core.BlocksPerWeek), 1},
		{"mid-epoch", int64(core.BlocksPerWeek) + 100, 1},
		{"epoch 3", int64(3 * core.BlocksPerWeek), 3},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.setBlockHeight(tc.blockHeight)
			s.Require().Equal(tc.expected, s.keeper.GetEpoch(s.ctx))
		})
	}
}

// TestUpdateIndicators, TestUpdateIndicators_ZeroTaxProceeds, and
// TestUpdateIndicators_MultiDenomTaxProceeds remain as separate methods
// because each requires different mock expectations (gomock AnyTimes
// persists across sub-tests).

func (s *KeeperTestSuite) TestUpdateIndicators() {
	s.setBlockHeight(0)

	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
		Return(math.NewInt(1000))

	// Tax proceeds: 500 usdr
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewCoin(core.MicroSDRDenom, math.NewInt(500))),
	}))

	// Seigniorage: initial=1000, current=900 → seigniorage=100
	s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(900)))

	// rewardWeight=5%, seigniorage=100 → seigniorageRewardsAmt=5 uark
	// Mock market conversion: 5 uark → 5 usdr (1:1 rate)
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyNewDec(5)), nil).
		AnyTimes()

	err := s.keeper.UpdateIndicators(s.ctx)
	s.Require().NoError(err)

	epochState, err := s.keeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().Equal(uint64(0), epochState.Epoch)
	s.Require().True(epochState.TaxReward.IsPositive())
	s.Require().Equal(math.NewInt(1000), epochState.TotalStakedArk)

	// Tax proceeds reset after UpdateIndicators
	proceeds, err := s.keeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(proceeds.TaxProceeds.IsZero())
}

func (s *KeeperTestSuite) TestUpdateIndicators_ZeroTaxProceeds() {
	s.setBlockHeight(0)

	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
		Return(math.NewInt(1000))

	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{}))

	// No seigniorage
	s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))

	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyZeroDec()), nil).
		AnyTimes()

	err := s.keeper.UpdateIndicators(s.ctx)
	s.Require().NoError(err)

	epochState, err := s.keeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().True(epochState.TaxReward.IsZero())
	s.Require().True(epochState.SeigniorageReward.IsZero())
}

func (s *KeeperTestSuite) TestUpdateIndicators_MultiDenomTaxProceeds() {
	s.setBlockHeight(0)

	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
		Return(math.NewInt(1000))

	// Tax proceeds in multiple denoms
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(
			sdk.NewCoin("uusd", math.NewInt(300)),
			sdk.NewCoin(core.MicroSDRDenom, math.NewInt(200)),
		),
	}))

	// No seigniorage
	s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))

	// Mock market: uusd → usdr conversion returns 300 usdr
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyNewDec(300)), nil).
		AnyTimes()

	err := s.keeper.UpdateIndicators(s.ctx)
	s.Require().NoError(err)

	epochState, err := s.keeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	// 300 (converted from uusd) + 200 (usdr, no conversion) = 500
	s.Require().True(epochState.TaxReward.Equal(math.LegacyNewDec(500)))
}

func (s *KeeperTestSuite) TestUpdateIndicators_ZeroBondedTokens() {
	s.setBlockHeight(0)

	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
		Return(math.ZeroInt())

	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewCoin(core.MicroSDRDenom, math.NewInt(500))),
	}))

	// No seigniorage
	s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))

	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyZeroDec()), nil).
		AnyTimes()

	err := s.keeper.UpdateIndicators(s.ctx)
	s.Require().NoError(err)

	epochState, err := s.keeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().True(epochState.TotalStakedArk.IsZero())
	// Tax reward is still computed (stored as SDR amount), but zero staked ark
	// means rolling average will skip this epoch in policy calculations
}
