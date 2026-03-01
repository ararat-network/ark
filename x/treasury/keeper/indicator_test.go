package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestGetEpoch() {
	// Block 0 → epoch 0
	s.setBlockHeight(0)
	s.Require().Equal(uint64(0), s.treasuryKeeper.GetEpoch(s.ctx))

	// Block BlocksPerWeek-1 → epoch 0
	s.setBlockHeight(int64(core.BlocksPerWeek) - 1)
	s.Require().Equal(uint64(0), s.treasuryKeeper.GetEpoch(s.ctx))

	// Block BlocksPerWeek → epoch 1
	s.setBlockHeight(int64(core.BlocksPerWeek))
	s.Require().Equal(uint64(1), s.treasuryKeeper.GetEpoch(s.ctx))

	// Block 3*BlocksPerWeek → epoch 3
	s.setBlockHeight(int64(3 * core.BlocksPerWeek))
	s.Require().Equal(uint64(3), s.treasuryKeeper.GetEpoch(s.ctx))
}

func (s *KeeperTestSuite) TestUpdateIndicators() {
	// Set up at epoch 0 (block 0)
	s.setBlockHeight(0)

	// Mock staking: 1000 bonded tokens
	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
		Return(math.NewInt(1000))

	// Set tax proceeds: 500 usdr
	s.Require().NoError(s.treasuryKeeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewCoin(core.MicroSDRDenom, math.NewInt(500))),
	}))

	// Seigniorage: initial=1000, current=900 → seigniorage=100
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(900)))

	// Seigniorage reward requires aligning uark to usdr
	// rewardWeight=5%, seigniorage=100 → seigniorageRewardsAmt=5 uark
	// Mock market conversion: 5 uark → 5 usdr (1:1 rate)
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyNewDec(5)), nil).
		AnyTimes()

	err := s.treasuryKeeper.UpdateIndicators(s.ctx)
	s.Require().NoError(err)

	// Verify epoch state was stored
	epochState, err := s.treasuryKeeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().Equal(uint64(0), epochState.Epoch)
	s.Require().True(epochState.TaxReward.IsPositive())
	s.Require().Equal(math.NewInt(1000), epochState.TotalStakedArk)

	// Verify tax proceeds were reset
	proceeds, err := s.treasuryKeeper.EpochTaxProceeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(proceeds.TaxProceeds.IsZero())
}

func (s *KeeperTestSuite) TestUpdateIndicators_ZeroTaxProceeds() {
	s.setBlockHeight(0)

	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
		Return(math.NewInt(1000))

	// No tax proceeds
	s.Require().NoError(s.treasuryKeeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{}))

	// No seigniorage
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))

	// alignCoins is still called for seigniorage rewards (0 uark → usdr conversion)
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyZeroDec()), nil).
		AnyTimes()

	err := s.treasuryKeeper.UpdateIndicators(s.ctx)
	s.Require().NoError(err)

	epochState, err := s.treasuryKeeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().True(epochState.TaxReward.IsZero())
	s.Require().True(epochState.SeigniorageReward.IsZero())
}

func (s *KeeperTestSuite) TestUpdateIndicators_MultiDenomTaxProceeds() {
	s.setBlockHeight(0)

	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).
		Return(math.NewInt(1000))

	// Tax proceeds in multiple denoms
	s.Require().NoError(s.treasuryKeeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(
			sdk.NewCoin("uusd", math.NewInt(300)),
			sdk.NewCoin(core.MicroSDRDenom, math.NewInt(200)),
		),
	}))

	// No seigniorage
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)))

	// Mock market: uusd → usdr conversion returns 300 usdr
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), core.MicroSDRDenom).
		Return(sdk.NewDecCoinFromDec(core.MicroSDRDenom, math.LegacyNewDec(300)), nil).
		AnyTimes()

	err := s.treasuryKeeper.UpdateIndicators(s.ctx)
	s.Require().NoError(err)

	epochState, err := s.treasuryKeeper.EpochStates.Get(s.ctx, 0)
	s.Require().NoError(err)
	// 300 (converted from uusd) + 200 (usdr, no conversion) = 500
	s.Require().True(epochState.TaxReward.Equal(math.LegacyNewDec(500)))
}
