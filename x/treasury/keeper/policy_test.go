package keeper_test

import (
	"bytes"
	"context"
	stderrors "errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/pkg/types"
	oracletypes "noah/x/oracle/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestUpdateTaxCap() {
	type oracleResponse struct {
		amount math.LegacyDec
		err    error
	}

	tests := []struct {
		name            string
		whitelist       oracletypes.TobinTaxes
		oracleResponses map[string]oracleResponse
		expectedCaps    sdk.Coins
		absentDenoms    []string
		expectLog       bool
	}{
		{
			name: "updates whitelisted denoms",
			whitelist: oracletypes.TobinTaxes{
				{Denom: "uusd"},
				{Denom: "ukrw"},
			},
			oracleResponses: map[string]oracleResponse{
				"uusd": {amount: math.LegacyNewDec(1500000)},
				"ukrw": {amount: math.LegacyNewDec(1300000000)},
			},
			expectedCaps: sdk.NewCoins(
				sdk.NewCoin("uusd", math.NewInt(1500000)),
				sdk.NewCoin("ukrw", math.NewInt(1300000000)),
			),
		},
		{
			name: "skips cap denom",
			whitelist: oracletypes.TobinTaxes{
				{Denom: core.MicroSDRDenom},
				{Denom: "uusd"},
			},
			oracleResponses: map[string]oracleResponse{
				"uusd": {amount: math.LegacyNewDec(1500000)},
			},
			expectedCaps: sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1500000))),
			absentDenoms: []string{
				core.MicroSDRDenom,
			},
		},
		{
			name:            "empty whitelist",
			whitelist:       oracletypes.TobinTaxes{},
			oracleResponses: map[string]oracleResponse{},
			expectedCaps:    sdk.Coins{},
		},
		{
			name: "skips denom when oracle conversion fails",
			whitelist: oracletypes.TobinTaxes{
				{Denom: "uusd"},
				{Denom: "ukrw"},
			},
			oracleResponses: map[string]oracleResponse{
				"uusd": {amount: math.LegacyNewDec(1500000)},
				"ukrw": {err: stderrors.New("missing oracle rate")},
			},
			expectedCaps: sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(1500000))),
			absentDenoms: []string{
				"ukrw",
			},
			expectLog: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			var logBuf bytes.Buffer
			if tc.expectLog {
				sdkCtx := sdk.UnwrapSDKContext(s.ctx)
				s.ctx = sdkCtx.WithLogger(log.NewLogger(&logBuf, log.OutputJSONOption()))
			}

			var storedDenoms []string
			err := s.keeper.TaxCaps.Walk(s.ctx, nil, func(denom string, _ math.Int) (bool, error) {
				storedDenoms = append(storedDenoms, denom)
				return false, nil
			})
			s.Require().NoError(err)
			for _, denom := range storedDenoms {
				s.Require().NoError(s.keeper.TaxCaps.Remove(s.ctx, denom))
			}

			s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).
				Return(tc.whitelist, nil)

			expectedOracleCalls := 0
			for _, denom := range tc.whitelist {
				if denom.Denom != core.MicroSDRDenom {
					expectedOracleCalls++
				}
			}
			if expectedOracleCalls > 0 {
				s.marketKeeper.EXPECT().
					ComputeOracleRate(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, _ sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
						res, ok := tc.oracleResponses[askDenom]
						s.Require().True(ok, "unexpected oracle conversion for denom %s", askDenom)
						if res.err != nil {
							return sdk.DecCoin{}, res.err
						}
						return sdk.NewDecCoinFromDec(askDenom, res.amount), nil
					}).
					Times(expectedOracleCalls)
			}

			newCaps, err := s.keeper.UpdateTaxCap(s.ctx)
			s.Require().NoError(err)
			s.Require().True(tc.expectedCaps.Equal(newCaps), "expected caps %s, got %s", tc.expectedCaps, newCaps)

			for _, expectedCap := range tc.expectedCaps {
				storedCap, err := s.keeper.TaxCaps.Get(s.ctx, expectedCap.Denom)
				s.Require().NoError(err)
				s.Require().Equal(expectedCap.Amount, storedCap)
			}

			for _, denom := range tc.absentDenoms {
				hasCap, err := s.keeper.TaxCaps.Has(s.ctx, denom)
				s.Require().NoError(err)
				s.Require().False(hasCap)
			}

			if tc.expectLog {
				logOutput := logBuf.String()
				s.Require().Contains(logOutput, "skipping tax cap update")
				s.Require().Contains(logOutput, "ukrw")
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
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
			},
			expectedRate: types.DefaultTaxRate.Add(types.DefaultTaxPolicy.ChangeRateMax),
		},
		{
			name:        "with revenue - applies mining increment",
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
			expectedRate: math.LegacyNewDecWithPrec(107, 5),
		},
		{
			name:        "rolling averages use long and short windows",
			blockHeight: int64(4 * core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(18), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(20), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
				2: {Epoch: 2, TaxReward: math.LegacyNewDec(27), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
				3: {Epoch: 3, TaxReward: math.LegacyNewDec(30), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
				4: {Epoch: 4, TaxReward: math.LegacyNewDec(30), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
			},
			expectedRate: types.DefaultTaxRate,
		},
		{
			name:        "rolling averages skip missing epochs",
			blockHeight: int64(4 * core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(79), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(107), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
				3: {Epoch: 3, TaxReward: math.LegacyNewDec(107), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
				4: {Epoch: 4, TaxReward: math.LegacyNewDec(107), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.OneInt()},
			},
			expectedRate: types.DefaultTaxRate,
		},
		{
			name:        "already at RateMax - stays clamped",
			blockHeight: int64(core.BlocksPerWeek),
			initialRate: func() *math.LegacyDec { r := types.DefaultTaxPolicy.RateMax; return &r }(),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
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
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyZeroDec(), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
			},
			expectedWeight: types.DefaultRewardWeight.Add(types.DefaultRewardPolicy.ChangeRateMax),
		},
		{
			name:          "high seigniorage - weight decreases",
			blockHeight:   int64(core.BlocksPerWeek),
			initialWeight: func() *math.LegacyDec { w := math.LegacyNewDecWithPrec(40, 2); return &w }(), // 40%
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(100), SeigniorageReward: math.LegacyNewDec(9900), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(100), SeigniorageReward: math.LegacyNewDec(9900), TotalStakedArk: math.NewInt(1000)},
			},
			expectedWeight: math.LegacyNewDecWithPrec(375, 3),
		},
		{
			name:        "balanced revenue - within bounds",
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(500), SeigniorageReward: math.LegacyNewDec(500), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(500), SeigniorageReward: math.LegacyNewDec(500), TotalStakedArk: math.NewInt(1000)},
			},
			expectedWeight: math.LegacyNewDecWithPrec(67, 3),
		},
		{
			name:        "zero seigniorage, non-zero tax - hikes to RateMax",
			blockHeight: int64(core.BlocksPerWeek),
			epochStates: map[uint64]types.EpochState{
				0: {Epoch: 0, TaxReward: math.LegacyNewDec(1000), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
				1: {Epoch: 1, TaxReward: math.LegacyNewDec(1000), SeigniorageReward: math.LegacyZeroDec(), TotalStakedArk: math.NewInt(1000)},
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
