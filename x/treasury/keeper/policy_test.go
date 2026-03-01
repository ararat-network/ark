package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	oracletypes "noah/x/oracle/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestUpdateTaxPolicy_ZeroRevenue() {
	// When rolling average indicator is zero (no revenue), tax rate should hike to RateMax
	s.setBlockHeight(int64(core.BlocksPerWeek))

	// Need epoch states for both epoch 0 and 1 (rolling average looks back)
	for i := uint64(0); i <= 1; i++ {
		s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, i, types.EpochState{
			Epoch:             i,
			TaxReward:         math.LegacyZeroDec(),
			SeigniorageReward: math.LegacyZeroDec(),
			TotalStakedArk:    math.NewInt(1000),
		}))
	}

	oldTaxRate, _ := s.treasuryKeeper.TaxRate.Get(s.ctx)

	newTaxRate, err := s.treasuryKeeper.UpdateTaxPolicy(s.ctx)
	s.Require().NoError(err)

	// With zero revenue, formula targets RateMax, but Clamp limits change per epoch.
	// New rate = oldRate + ChangeRateMax (clamped increase)
	params, _ := s.treasuryKeeper.Params.Get(s.ctx)
	expected := oldTaxRate.Add(params.TaxPolicy.ChangeRateMax)
	s.Require().True(newTaxRate.Equal(expected))
	s.Require().True(newTaxRate.GT(oldTaxRate))
}

func (s *KeeperTestSuite) TestUpdateTaxPolicy_WithRevenue() {
	// Set up multiple epochs of data so rolling averages are meaningful
	s.setBlockHeight(int64(4 * core.BlocksPerWeek))

	for i := uint64(0); i <= 4; i++ {
		s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, i, types.EpochState{
			Epoch:             i,
			TaxReward:         math.LegacyNewDec(1000),
			SeigniorageReward: math.LegacyNewDec(500),
			TotalStakedArk:    math.NewInt(100000),
		}))
	}

	newTaxRate, err := s.treasuryKeeper.UpdateTaxPolicy(s.ctx)
	s.Require().NoError(err)
	s.Require().True(newTaxRate.IsPositive())

	// Verify it's within bounds
	params, _ := s.treasuryKeeper.Params.Get(s.ctx)
	s.Require().True(newTaxRate.GTE(params.TaxPolicy.RateMin))
	s.Require().True(newTaxRate.LTE(params.TaxPolicy.RateMax))
}

func (s *KeeperTestSuite) TestUpdateRewardPolicy_ZeroRevenue() {
	// When total revenue is zero, reward weight should hike to RateMax
	s.setBlockHeight(int64(core.BlocksPerWeek))

	// Need epoch states for both epoch 0 and 1 (sumIndicator looks back)
	for i := uint64(0); i <= 1; i++ {
		s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, i, types.EpochState{
			Epoch:             i,
			TaxReward:         math.LegacyZeroDec(),
			SeigniorageReward: math.LegacyZeroDec(),
			TotalStakedArk:    math.NewInt(1000),
		}))
	}

	oldWeight, _ := s.treasuryKeeper.RewardWeight.Get(s.ctx)

	newRewardWeight, err := s.treasuryKeeper.UpdateRewardPolicy(s.ctx)
	s.Require().NoError(err)

	// With zero revenue, formula targets RateMax, but Clamp limits change per epoch.
	// New weight = oldWeight + ChangeRateMax (clamped increase)
	params, _ := s.treasuryKeeper.Params.Get(s.ctx)
	expected := oldWeight.Add(params.RewardPolicy.ChangeRateMax)
	s.Require().True(newRewardWeight.Equal(expected))
	s.Require().True(newRewardWeight.GT(oldWeight))
}

func (s *KeeperTestSuite) TestUpdateRewardPolicy_HighSeigniorage() {
	// When seigniorage is high relative to total, reward weight should decrease
	s.setBlockHeight(int64(core.BlocksPerWeek))

	// Set up: seigniorage >> tax rewards → SB > SBTarget → weight decreases
	s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, 0, types.EpochState{
		Epoch:             0,
		TaxReward:         math.LegacyNewDec(100),
		SeigniorageReward: math.LegacyNewDec(9900),
		TotalStakedArk:    math.NewInt(1000),
	}))
	s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, 1, types.EpochState{
		Epoch:             1,
		TaxReward:         math.LegacyNewDec(100),
		SeigniorageReward: math.LegacyNewDec(9900),
		TotalStakedArk:    math.NewInt(1000),
	}))

	// Set a high reward weight so we can observe it decrease
	s.Require().NoError(s.treasuryKeeper.RewardWeight.Set(s.ctx, math.LegacyNewDecWithPrec(40, 2))) // 40%

	newRewardWeight, err := s.treasuryKeeper.UpdateRewardPolicy(s.ctx)
	s.Require().NoError(err)

	// SB = 9900/10000 = 0.99, SBTarget = 0.67
	// newWeight = 0.40 * 0.67/0.99 ≈ 0.27
	// Should be lower than previous weight
	s.Require().True(newRewardWeight.LT(math.LegacyNewDecWithPrec(40, 2)))
	s.Require().True(newRewardWeight.IsPositive())
}

func (s *KeeperTestSuite) TestUpdateRewardPolicy_WithinBounds() {
	s.setBlockHeight(int64(core.BlocksPerWeek))

	s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, 0, types.EpochState{
		Epoch:             0,
		TaxReward:         math.LegacyNewDec(500),
		SeigniorageReward: math.LegacyNewDec(500),
		TotalStakedArk:    math.NewInt(1000),
	}))
	s.Require().NoError(s.treasuryKeeper.EpochStates.Set(s.ctx, 1, types.EpochState{
		Epoch:             1,
		TaxReward:         math.LegacyNewDec(500),
		SeigniorageReward: math.LegacyNewDec(500),
		TotalStakedArk:    math.NewInt(1000),
	}))

	newRewardWeight, err := s.treasuryKeeper.UpdateRewardPolicy(s.ctx)
	s.Require().NoError(err)

	params, _ := s.treasuryKeeper.Params.Get(s.ctx)
	s.Require().True(newRewardWeight.GTE(params.RewardPolicy.RateMin))
	s.Require().True(newRewardWeight.LTE(params.RewardPolicy.RateMax))
}

func (s *KeeperTestSuite) TestUpdateTaxCap() {
	// Set up oracle whitelist with denoms
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{
			{Name: "uusd"},
			{Name: "ukrw"},
		})

	// Mock market rate: 1 SDR cap → converted to each denom
	// Default cap = 1 SDR (1000000 usdr)
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), "uusd").
		Return(sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1500000)), nil)
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), "ukrw").
		Return(sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(1300000000)), nil)

	newCaps, err := s.treasuryKeeper.UpdateTaxCap(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(newCaps, 2)

	// Verify caps stored
	usdCap, err := s.treasuryKeeper.TaxCaps.Get(s.ctx, "uusd")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1500000), usdCap)

	krwCap, err := s.treasuryKeeper.TaxCaps.Get(s.ctx, "ukrw")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1300000000), krwCap)
}

func (s *KeeperTestSuite) TestUpdateTaxCap_SkipsCapDenom() {
	// The cap's own denom (usdr) should be skipped
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{
			{Name: core.MicroSDRDenom}, // should be skipped
			{Name: "uusd"},
		})

	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), "uusd").
		Return(sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1500000)), nil)

	newCaps, err := s.treasuryKeeper.UpdateTaxCap(s.ctx)
	s.Require().NoError(err)
	// Only uusd should get a cap, usdr is skipped
	s.Require().Len(newCaps, 1)
	s.Require().Equal("uusd", newCaps[0].Denom)
}
