package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestComputeAndSetTaxCaps() {
	tests := []struct {
		name         string
		tobinTaxes   oracletypes.TobinTaxes
		rates        oracletypes.RateSnapshot
		expectedCaps sdk.Coins
		expectErr    bool
	}{
		{
			name: "updates whitelisted denoms",
			tobinTaxes: oracletypes.TobinTaxes{
				{Denom: "uusd"},
				{Denom: "ukrw"},
			},
			rates: oracletypes.RateSnapshot{
				chain.MicroSDRDenom: math.LegacyOneDec(),
				"uusd":              math.LegacyMustNewDecFromStr("1.5"),
				"ukrw":              math.LegacyNewDec(1300),
			},
			expectedCaps: sdk.NewCoins(
				sdk.NewCoin("uusd", math.NewInt(1500000)),
				sdk.NewCoin("ukrw", math.NewInt(1300000000)),
			),
		},
		{
			name: "skips cap denom",
			tobinTaxes: oracletypes.TobinTaxes{
				{Denom: chain.MicroSDRDenom},
				{Denom: "uusd"},
			},
			rates: oracletypes.RateSnapshot{
				chain.MicroSDRDenom: math.LegacyOneDec(),
				"uusd":              math.LegacyMustNewDecFromStr("1.5"),
			},
			expectedCaps: sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1500000))),
		},
		{
			name:         "empty whitelist",
			tobinTaxes:   oracletypes.TobinTaxes{},
			rates:        oracletypes.RateSnapshot{},
			expectedCaps: sdk.Coins{},
		},
		{
			name: "conversion failure prevents partial update",
			tobinTaxes: oracletypes.TobinTaxes{
				{Denom: "uusd"},
				{Denom: "ukrw"},
			},
			rates: oracletypes.RateSnapshot{
				chain.MicroSDRDenom: math.LegacyOneDec(),
				"uusd":              math.LegacyMustNewDecFromStr("1.5"),
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			var storedDenoms []string
			err := s.keeper.TaxCaps.Walk(s.ctx, nil, func(denom string, _ math.Int) (bool, error) {
				storedDenoms = append(storedDenoms, denom)
				return false, nil
			})
			s.Require().NoError(err)
			for _, denom := range storedDenoms {
				s.Require().NoError(s.keeper.TaxCaps.Remove(s.ctx, denom))
			}

			newCaps, err := s.keeper.ComputeTaxCaps(s.ctx, tc.tobinTaxes, tc.rates)
			if tc.expectErr {
				s.Require().Error(err)
				hasUSDCap, hasErr := s.keeper.TaxCaps.Has(s.ctx, "uusd")
				s.Require().NoError(hasErr)
				s.Require().False(hasUSDCap)
				return
			}
			s.Require().NoError(err)
			s.Require().True(tc.expectedCaps.Equal(newCaps), "expected caps %s, got %s", tc.expectedCaps, newCaps)
			s.Require().NoError(s.keeper.SetTaxCaps(s.ctx, newCaps))

			for _, expectedCap := range tc.expectedCaps {
				storedCap, err := s.keeper.TaxCaps.Get(s.ctx, expectedCap.Denom)
				s.Require().NoError(err)
				s.Require().Equal(expectedCap.Amount, storedCap)
			}

		})
	}
}

func (s *KeeperTestSuite) TestUpdateTaxPolicy() {
	tests := []struct {
		name         string
		blockHeight  int64
		initialRate  *math.LegacyDec // nil = use default
		epochStates  map[uint64]types.EpochState
		expectedRate math.LegacyDec
	}{
		{
			name:        "zero revenue - rate increases by ChangeRateMax",
			blockHeight: int64(chain.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
			},
			expectedRate: types.DefaultTaxRate.Add(types.DefaultTaxPolicy.ChangeRateMax),
		},
		{
			name:        "with revenue - applies mining increment",
			blockHeight: int64(4 * chain.BlocksPerWeek),
			epochStates: func() map[uint64]types.EpochState {
				m := make(map[uint64]types.EpochState)
				for i := uint64(0); i <= 4; i++ {
					m[i] = types.EpochState{
						Epoch:             i,
						TaxReward:         math.LegacyNewDec(1000),
						SeigniorageReward: math.LegacyNewDec(500),
						TotalStakedNoah:   math.NewInt(100000),
					}
				}
				return m
			}(),
			expectedRate: math.LegacyNewDecWithPrec(107, 5),
		},
		{
			name:        "rolling averages use long and short windows",
			blockHeight: int64(4 * chain.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(18), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(20), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
				2: {Epoch: 2, TaxReward: math.LegacyNewDec(27), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
				3: {Epoch: 3, TaxReward: math.LegacyNewDec(30), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
				4: {Epoch: 4, TaxReward: math.LegacyNewDec(30), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
			},
			expectedRate: types.DefaultTaxRate,
		},
		{
			name:        "rolling averages skip missing epochs",
			blockHeight: int64(4 * chain.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(79), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(107), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
				3: {Epoch: 3, TaxReward: math.LegacyNewDec(107), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
				4: {Epoch: 4, TaxReward: math.LegacyNewDec(107), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.OneInt()},
			},
			expectedRate: types.DefaultTaxRate,
		},
		{
			name:        "already at RateMax - stays clamped",
			blockHeight: int64(chain.BlocksPerWeek),
			initialRate: func() *math.LegacyDec { r := types.DefaultTaxPolicy.RateMax; return &r }(),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
			},
			expectedRate: types.DefaultTaxPolicy.RateMax,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset to defaults — sub-tests share state within a suite method
			s.Require().NoError(s.keeper.TaxRate.Set(s.ctx, types.DefaultTaxRate))
			s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))
			var epochs []uint64
			err := s.keeper.EpochStates.Walk(s.ctx, nil, func(epoch uint64, _ types.EpochState) (bool, error) {
				epochs = append(epochs, epoch)
				return false, nil
			})
			s.Require().NoError(err)
			for _, epoch := range epochs {
				s.Require().NoError(s.keeper.EpochStates.Remove(s.ctx, epoch))
			}

			s.setBlockHeight(tc.blockHeight)
			if tc.initialRate != nil {
				s.Require().NoError(s.keeper.TaxRate.Set(s.ctx, *tc.initialRate))
			}
			for epoch, state := range tc.epochStates {
				s.Require().NoError(s.keeper.EpochStates.Set(s.ctx, epoch, state))
			}

			newRate, err := s.keeper.UpdateTaxPolicy(s.ctx)
			s.Require().NoError(err)
			s.Require().True(newRate.Equal(tc.expectedRate),
				"expected tax rate %s, got %s", tc.expectedRate, newRate)
		})
	}
}

func (s *KeeperTestSuite) TestUpdateRewardPolicy() {
	tests := []struct {
		name           string
		blockHeight    int64
		initialWeight  *math.LegacyDec // nil = use default
		mutateParams   func(*types.Params)
		epochStates    map[uint64]types.EpochState
		expectedWeight math.LegacyDec
	}{
		{
			name:        "zero revenue - weight increases by ChangeRateMax",
			blockHeight: int64(chain.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
			},
			expectedWeight: types.DefaultRewardWeight.Add(types.DefaultRewardPolicy.ChangeRateMax),
		},
		{
			name:          "high seigniorage - weight decreases",
			blockHeight:   int64(chain.BlocksPerWeek),
			initialWeight: func() *math.LegacyDec { w := math.LegacyNewDecWithPrec(40, 2); return &w }(), // 40%
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(100), SeigniorageReward: math.LegacyNewDec(9900), TotalStakedNoah: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(100), SeigniorageReward: math.LegacyNewDec(9900), TotalStakedNoah: math.NewInt(1000)},
			},
			expectedWeight: math.LegacyNewDecWithPrec(375, 3),
		},
		{
			name:        "balanced revenue - within bounds",
			blockHeight: int64(chain.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(500), SeigniorageReward: math.LegacyNewDec(500), TotalStakedNoah: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(500), SeigniorageReward: math.LegacyNewDec(500), TotalStakedNoah: math.NewInt(1000)},
			},
			expectedWeight: math.LegacyNewDecWithPrec(67, 3),
		},
		{
			name:        "zero seigniorage, non-zero tax - hikes to RateMax",
			blockHeight: int64(chain.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(1000), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(1000), SeigniorageReward: math.LegacyZeroDec(), TotalStakedNoah: math.NewInt(1000)},
			},
			expectedWeight: types.DefaultRewardWeight.Add(types.DefaultRewardPolicy.ChangeRateMax),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset to defaults — sub-tests share state within a suite method
			s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, types.DefaultRewardWeight))
			params := types.DefaultParams()
			if tc.mutateParams != nil {
				tc.mutateParams(&params)
			}
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			var epochs []uint64
			err := s.keeper.EpochStates.Walk(s.ctx, nil, func(epoch uint64, _ types.EpochState) (bool, error) {
				epochs = append(epochs, epoch)
				return false, nil
			})
			s.Require().NoError(err)
			for _, epoch := range epochs {
				s.Require().NoError(s.keeper.EpochStates.Remove(s.ctx, epoch))
			}

			s.setBlockHeight(tc.blockHeight)
			if tc.initialWeight != nil {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, *tc.initialWeight))
			}
			for epoch, state := range tc.epochStates {
				s.Require().NoError(s.keeper.EpochStates.Set(s.ctx, epoch, state))
			}

			newWeight, err := s.keeper.UpdateRewardPolicy(s.ctx)
			s.Require().NoError(err)
			s.Require().True(newWeight.Equal(tc.expectedWeight),
				"expected reward weight %s, got %s", tc.expectedWeight, newWeight)
		})
	}
}
