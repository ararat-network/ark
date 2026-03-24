package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	oracletypes "noah/x/oracle/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestUpdateTaxPolicy() {
	tests := []struct {
		name        string
		blockHeight int64
		initialRate *math.LegacyDec // nil = use default
		epochStates map[uint64]types.EpochState
		validate    func(newRate math.LegacyDec)
	}{
		{
			name:        "zero revenue — rate increases by ChangeRateMax",
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
			},
			validate: func(newRate math.LegacyDec) {
				params, _ := s.keeper.Params.Get(s.ctx)
				expected := types.DefaultTaxRate.Add(params.TaxPolicy.ChangeRateMax)
				s.Require().True(newRate.Equal(expected))
				s.Require().True(newRate.GT(types.DefaultTaxRate))
			},
		},
		{
			name:        "with revenue — stays within bounds",
			blockHeight: int64(4 * core.BlocksPerWeek),
			epochStates: func() map[uint64]types.EpochState {
				m := make(map[uint64]types.EpochState)
				for i := uint64(0); i <= 4; i++ {
					m[i] = types.EpochState{
						Epoch:             i,
						TaxReward:         math.LegacyNewDec(1000),
						SeigniorageReward: math.LegacyNewDec(500),
						TotalStakedArk:    math.NewInt(100000),
					}
				}
				return m
			}(),
			validate: func(newRate math.LegacyDec) {
				s.Require().True(newRate.IsPositive())
				params, _ := s.keeper.Params.Get(s.ctx)
				s.Require().True(newRate.GTE(params.TaxPolicy.RateMin))
				s.Require().True(newRate.LTE(params.TaxPolicy.RateMax))
			},
		},
		{
			name:        "already at RateMax — stays clamped",
			blockHeight: int64(core.BlocksPerWeek),
			initialRate: func() *math.LegacyDec { r := types.DefaultTaxPolicy.RateMax; return &r }(),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
			},
			validate: func(newRate math.LegacyDec) {
				s.Require().True(newRate.Equal(types.DefaultTaxPolicy.RateMax))
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset to defaults — sub-tests share state within a suite method
			s.Require().NoError(s.keeper.TaxRate.Set(s.ctx, types.DefaultTaxRate))
			s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))

			s.setBlockHeight(tc.blockHeight)
			if tc.initialRate != nil {
				s.Require().NoError(s.keeper.TaxRate.Set(s.ctx, *tc.initialRate))
			}
			for epoch, state := range tc.epochStates {
				s.Require().NoError(s.keeper.EpochStates.Set(s.ctx, epoch, state))
			}

			newRate, err := s.keeper.UpdateTaxPolicy(s.ctx)
			s.Require().NoError(err)
			tc.validate(newRate)
		})
	}
}

func (s *KeeperTestSuite) TestUpdateRewardPolicy() {
	tests := []struct {
		name          string
		blockHeight   int64
		initialWeight *math.LegacyDec // nil = use default
		epochStates   map[uint64]types.EpochState
		validate      func(newWeight math.LegacyDec)
	}{
		{
			name:        "zero revenue — weight increases by ChangeRateMax",
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
			},
			validate: func(newWeight math.LegacyDec) {
				params, _ := s.keeper.Params.Get(s.ctx)
				expected := types.DefaultRewardWeight.Add(params.RewardPolicy.ChangeRateMax)
				s.Require().True(newWeight.Equal(expected))
				s.Require().True(newWeight.GT(types.DefaultRewardWeight))
			},
		},
		{
			name:          "high seigniorage — weight decreases",
			blockHeight:   int64(core.BlocksPerWeek),
			initialWeight: func() *math.LegacyDec { w := math.LegacyNewDecWithPrec(40, 2); return &w }(), // 40%
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(100), SeigniorageReward: math.LegacyNewDec(9900), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(100), SeigniorageReward: math.LegacyNewDec(9900), TotalStakedArk: math.NewInt(1000)},
			},
			validate: func(newWeight math.LegacyDec) {
				// SB = 9900/10000 = 0.99, SBTarget = 0.67
				// newWeight = 0.40 * 0.67/0.99 ≈ 0.27
				s.Require().True(newWeight.LT(math.LegacyNewDecWithPrec(40, 2)))
				s.Require().True(newWeight.IsPositive())
			},
		},
		{
			name:        "balanced revenue — within bounds",
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(500), SeigniorageReward: math.LegacyNewDec(500), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(500), SeigniorageReward: math.LegacyNewDec(500), TotalStakedArk: math.NewInt(1000)},
			},
			validate: func(newWeight math.LegacyDec) {
				params, _ := s.keeper.Params.Get(s.ctx)
				s.Require().True(newWeight.GTE(params.RewardPolicy.RateMin))
				s.Require().True(newWeight.LTE(params.RewardPolicy.RateMax))
			},
		},
		{
			name:        "zero seigniorage, non-zero tax — hikes to RateMax",
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(1000), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(1000), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
			},
			validate: func(newWeight math.LegacyDec) {
				// seigniorageSum == 0 → targets RateMax, clamped by ChangeRateMax
				params, _ := s.keeper.Params.Get(s.ctx)
				expected := types.DefaultRewardWeight.Add(params.RewardPolicy.ChangeRateMax)
				s.Require().True(newWeight.Equal(expected))
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset to defaults — sub-tests share state within a suite method
			s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, types.DefaultRewardWeight))
			s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))

			s.setBlockHeight(tc.blockHeight)
			if tc.initialWeight != nil {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, *tc.initialWeight))
			}
			for epoch, state := range tc.epochStates {
				s.Require().NoError(s.keeper.EpochStates.Set(s.ctx, epoch, state))
			}

			newWeight, err := s.keeper.UpdateRewardPolicy(s.ctx)
			s.Require().NoError(err)
			tc.validate(newWeight)
		})
	}
}

// TestUpdateTaxCap* remain as separate methods because each requires
// different oracle/market mock expectations.

func (s *KeeperTestSuite) TestUpdateTaxCap() {
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{
			{Name: "uusd"},
			{Name: "ukrw"},
		})

	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), "uusd").
		Return(sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1500000)), nil)
	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), "ukrw").
		Return(sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(1300000000)), nil)

	newCaps, err := s.keeper.UpdateTaxCap(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(newCaps, 2)

	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, "uusd")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1500000), usdCap)

	krwCap, err := s.keeper.TaxCaps.Get(s.ctx, "ukrw")
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1300000000), krwCap)
}

func (s *KeeperTestSuite) TestUpdateTaxCap_SkipsCapDenom() {
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{
			{Name: core.MicroSDRDenom},
			{Name: "uusd"},
		})

	s.marketKeeper.EXPECT().
		ComputeOracleRate(gomock.Any(), gomock.Any(), "uusd").
		Return(sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1500000)), nil)

	newCaps, err := s.keeper.UpdateTaxCap(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(newCaps, 1)
	s.Require().Equal("uusd", newCaps[0].Denom)
}

func (s *KeeperTestSuite) TestUpdateTaxCap_EmptyWhitelist() {
	s.oracleKeeper.EXPECT().Whitelist(gomock.Any()).
		Return(oracletypes.DenomList{})

	newCaps, err := s.keeper.UpdateTaxCap(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(newCaps)
}
