package keeper_test

import (
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/keeper"
	"ark/x/treasury/types"
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
	tests := []struct {
		name string
		caps []types.TaxCap
	}{
		{
			name: "with tax caps",
			caps: []types.TaxCap{
				{Denom: "ukrw", TaxCap: math.NewInt(1300000000)},
				{Denom: "uusd", TaxCap: math.NewInt(1000000)},
			},
		},
		{
			name: "empty",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			var denoms []string
			err := s.keeper.TaxCaps.Walk(s.ctx, nil, func(denom string, _ math.Int) (bool, error) {
				denoms = append(denoms, denom)
				return false, nil
			})
			s.Require().NoError(err)

			for _, denom := range denoms {
				s.Require().NoError(s.keeper.TaxCaps.Remove(s.ctx, denom))
			}

			expected := make(map[string]math.Int, len(tc.caps))
			for _, cap := range tc.caps {
				s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, cap.Denom, cap.TaxCap))
				expected[cap.Denom] = cap.TaxCap
			}

			res, err := s.queryClient.TaxCaps(s.ctx, &types.QueryTaxCapsRequest{})
			s.Require().NoError(err)
			s.Require().NotNil(res)
			s.Require().Len(res.TaxCaps, len(tc.caps))

			for _, cap := range res.TaxCaps {
				expectedCap, ok := expected[cap.Denom]
				s.Require().True(ok, "unexpected tax cap denom %s", cap.Denom)
				s.Require().Equal(expectedCap, cap.TaxCap)
			}
		})
	}
}

func (s *KeeperTestSuite) TestQueryRewardWeight() {
	res, err := s.queryClient.RewardWeight(s.ctx, &types.QueryRewardWeightRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(rewardWeight.Equal(res.RewardWeight))
}

func (s *KeeperTestSuite) TestQuerySeigniorageProceeds() {
	s.Require().NoError(s.keeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(s.ctx, chain.MicroNoahDenom).
		Return(sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(800)))

	res, err := s.queryClient.SeigniorageProceeds(s.ctx, &types.QuerySeigniorageProceedsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(math.NewInt(200), res.SeigniorageProceeds)
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

func (s *KeeperTestSuite) TestQueryIndicators() {
	tests := []struct {
		name          string
		blockHeight   int64
		totalStaked   math.Int
		taxProceeds   sdk.Coins
		epochStates   []types.EpochState
		expectedYear  math.LegacyDec
		expectedMonth math.LegacyDec
	}{
		{
			name:          "epoch 0 zero stake",
			totalStaked:   math.ZeroInt(),
			expectedYear:  math.LegacyZeroDec(),
			expectedMonth: math.LegacyZeroDec(),
		},
		{
			name:          "epoch 0 with tax proceeds",
			totalStaked:   math.NewInt(1000),
			taxProceeds:   sdk.NewCoins(sdk.NewCoin(chain.MicroSDRDenom, math.NewInt(500))),
			expectedYear:  math.LegacyNewDecWithPrec(5, 1),
			expectedMonth: math.LegacyNewDecWithPrec(5, 1),
		},
		{
			name:        "epoch 1 blends previous epoch with current epoch",
			blockHeight: int64(chain.BlocksPerWeek),
			totalStaked: math.NewInt(1000),
			taxProceeds: sdk.NewCoins(sdk.NewCoin(chain.MicroSDRDenom, math.NewInt(200))),
			epochStates: []types.EpochState{
				{
					Epoch:             0,
					TaxReward:         math.LegacyNewDec(100),
					SeigniorageReward: math.LegacyZeroDec(),
					TotalStakedNoah:   math.NewInt(1000),
				},
			},
			expectedYear:  math.LegacyNewDecWithPrec(15, 2),
			expectedMonth: math.LegacyNewDecWithPrec(15, 2),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.setBlockHeight(tc.blockHeight)
			s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
				TaxProceeds: tc.taxProceeds,
			}))

			var epochs []uint64
			err := s.keeper.EpochStates.Walk(s.ctx, nil, func(epoch uint64, _ types.EpochState) (bool, error) {
				epochs = append(epochs, epoch)
				return false, nil
			})
			s.Require().NoError(err)

			for _, epoch := range epochs {
				s.Require().NoError(s.keeper.EpochStates.Remove(s.ctx, epoch))
			}

			for _, epochState := range tc.epochStates {
				s.Require().NoError(s.keeper.EpochStates.Set(s.ctx, epochState.Epoch, epochState))
			}

			s.stakingKeeper.EXPECT().TotalValidatorPower(gomock.Any()).Return(tc.totalStaked, nil)

			qs := keeper.NewQueryServerImpl(s.keeper)
			res, err := qs.Indicators(s.ctx, &types.QueryIndicatorsRequest{})
			s.Require().NoError(err)
			s.Require().True(res.TRAYear.Equal(tc.expectedYear), "expected TRAYear %s, got %s", tc.expectedYear, res.TRAYear)
			s.Require().True(res.TRAMonth.Equal(tc.expectedMonth), "expected TRAMonth %s, got %s", tc.expectedMonth, res.TRAMonth)
		})
	}
}

func (s *KeeperTestSuite) TestQueryIndicatorsStaleRate() {
	s.Require().NoError(s.keeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 500)),
	}))
	s.stakingKeeper.EXPECT().TotalValidatorPower(gomock.Any()).Return(math.NewInt(1000), nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroUSDDenom,
		chain.MicroSDRDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)

	_, err := keeper.NewQueryServerImpl(s.keeper).Indicators(s.ctx, &types.QueryIndicatorsRequest{})
	s.Require().Error(err)
	s.Require().Equal(codes.FailedPrecondition, status.Code(err))
}
