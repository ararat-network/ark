package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/keeper"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	res, err := s.queryClient.Params(s.ctx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(params, res.Params)
}

func (s *KeeperTestSuite) TestQueryTaxRate() {
	res, err := s.queryClient.TaxRate(s.ctx, &types.QueryTaxRateRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	taxRate, err := s.keeper.TaxRate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(taxRate.Equal(res.TaxRate))
}

func (s *KeeperTestSuite) TestQueryTaxCap() {
	// Pre-set a cap for the "found" case
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, "uusd", math.NewInt(1000000)))

	tests := []struct {
		name      string
		req       *types.QueryTaxCapRequest
		expectErr string
		validate  func(*types.QueryTaxCapResponse)
	}{
		{
			name: "found",
			req:  &types.QueryTaxCapRequest{Denom: "uusd"},
			validate: func(res *types.QueryTaxCapResponse) {
				s.Require().Equal(math.NewInt(1000000), res.TaxCap)
			},
		},
		{
			name:      "not found",
			req:       &types.QueryTaxCapRequest{Denom: "ukrw"},
			expectErr: "tax cap not found",
		},
		{
			name:      "invalid denom",
			req:       &types.QueryTaxCapRequest{Denom: ""},
			expectErr: "invalid denom",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			res, err := s.queryClient.TaxCap(s.ctx, tc.req)
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().ErrorContains(err, tc.expectErr)
			} else {
				s.Require().NoError(err)
				s.Require().NotNil(res)
				tc.validate(res)
			}
		})
	}
}

func (s *KeeperTestSuite) TestQueryTaxCaps() {
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, "uusd", math.NewInt(1000000)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, "ukrw", math.NewInt(1300000000)))

	res, err := s.queryClient.TaxCaps(s.ctx, &types.QueryTaxCapsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Len(res.TaxCaps, 2)
}

func (s *KeeperTestSuite) TestQueryTaxCaps_Empty() {
	res, err := s.queryClient.TaxCaps(s.ctx, &types.QueryTaxCapsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Empty(res.TaxCaps)
}

func (s *KeeperTestSuite) TestQueryRewardWeight() {
	res, err := s.queryClient.RewardWeight(s.ctx, &types.QueryRewardWeightRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(rewardWeight.Equal(res.RewardWeight))
}

func (s *KeeperTestSuite) TestQueryTaxProceeds() {
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(5000))),
	}))

	res, err := s.queryClient.TaxProceeds(s.ctx, &types.QueryTaxProceedsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(math.NewInt(5000), res.TaxProceeds.AmountOf("uusd"))
}

func (s *KeeperTestSuite) TestQuerySeigniorageProceeds() {
	s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(800)))

	res, err := s.queryClient.SeigniorageProceeds(s.ctx, &types.QuerySeigniorageProceedsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(math.NewInt(200), res.SeigniorageProceeds)
}

func (s *KeeperTestSuite) TestQuerySeigniorageProceeds_Zero() {
	// Supply unchanged from initial issuance → zero seigniorage
	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000000000)))

	res, err := s.queryClient.SeigniorageProceeds(s.ctx, &types.QuerySeigniorageProceedsRequest{})
	s.Require().NoError(err)
	s.Require().True(res.SeigniorageProceeds.IsZero())
}

// QueryIndicators tests use the query server directly (not gRPC client)
// because the gRPC test helper's context doesn't reflect block height changes.

func (s *KeeperTestSuite) TestQueryIndicators_Epoch0_ZeroStake() {
	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).Return(math.ZeroInt())

	qs := keeper.NewQueryServerImpl(s.keeper)
	res, err := qs.Indicators(s.ctx, &types.QueryIndicatorsRequest{})
	s.Require().NoError(err)
	s.Require().True(res.TRAYear.IsZero())
	s.Require().True(res.TRAMonth.IsZero())
}

func (s *KeeperTestSuite) TestQueryIndicators_Epoch0_WithTaxProceeds() {
	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).Return(math.NewInt(1000))
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewCoin(core.MicroSDRDenom, math.NewInt(500))),
	}))

	qs := keeper.NewQueryServerImpl(s.keeper)
	res, err := qs.Indicators(s.ctx, &types.QueryIndicatorsRequest{})
	s.Require().NoError(err)
	// TRA = 500/1000 = 0.5
	expected := math.LegacyNewDecWithPrec(5, 1)
	s.Require().True(res.TRAYear.Equal(expected), "expected %s, got %s", expected, res.TRAYear)
	s.Require().True(res.TRAMonth.Equal(expected), "expected %s, got %s", expected, res.TRAMonth)
}

func (s *KeeperTestSuite) TestQueryIndicators_Epoch1() {
	// Populate epoch 0 state
	s.Require().NoError(s.keeper.EpochStates.Set(s.ctx, 0, types.EpochState{
		Epoch:             0,
		TaxReward:         math.LegacyNewDec(100),
		SeigniorageReward: math.LegacyZeroDec(),
		TotalStakedArk:    math.NewInt(1000),
	}))

	s.setBlockHeight(int64(core.BlocksPerWeek))

	s.stakingKeeper.EXPECT().TotalBondedTokens(gomock.Any()).Return(math.NewInt(1000))
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewCoin(core.MicroSDRDenom, math.NewInt(200))),
	}))

	qs := keeper.NewQueryServerImpl(s.keeper)
	res, err := qs.Indicators(s.ctx, &types.QueryIndicatorsRequest{})
	s.Require().NoError(err)

	// Previous epoch rolling average (epoch 0):
	//   TRA = 100/1000 = 0.1, counted=1 → avg = 0.1
	// Current: TR=200, TSA=1000 → TR/TSA = 0.2
	// computedEpochForYear = min(WindowLong-1=51, epoch=1) = 1
	// traYear = 0.1*1 + 0.2 / (1+1) = 0.3/2 = 0.15
	// Same for traMonth (computedEpochForMonth = min(WindowShort-1=3, 1) = 1)
	expected := math.LegacyNewDecWithPrec(15, 2)
	s.Require().True(res.TRAYear.Equal(expected), "expected TRAYear %s, got %s", expected, res.TRAYear)
	s.Require().True(res.TRAMonth.Equal(expected), "expected TRAMonth %s, got %s", expected, res.TRAMonth)
}
