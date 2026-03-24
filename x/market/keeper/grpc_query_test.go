package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	core "noah/types"
	"noah/x/market/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	res, err := s.queryClient.Params(s.ctx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Compare with keeper state
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(params, res.Params)
}

func (s *KeeperTestSuite) TestQuerySwap() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QuerySwapRequest
		expectErr string
		validate  func(*types.QuerySwapResponse)
	}{
		{
			name: "valid noah-to-noah swap",
			setup: func() {
				s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
					Return(math.LegacyOneDec(), nil).AnyTimes()
				s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "ukrw").
					Return(math.LegacyNewDec(1300), nil).AnyTimes()
				s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
					Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()
				s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "uusd").
					Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes()
				s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "ukrw").
					Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes()
			},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000uusd",
				AskDenom:  "ukrw",
			},
			validate: func(res *types.QuerySwapResponse) {
				s.Require().Equal("ukrw", res.ReturnCoin.Denom)
				s.Require().True(res.ReturnCoin.Amount.IsPositive())
			},
		},
		{
			name:  "recursive swap",
			setup: func() {},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000uusd",
				AskDenom:  "uusd",
			},
			expectErr: "recursive swap",
		},
		{
			name:  "empty offer coin",
			setup: func() {},
			req: &types.QuerySwapRequest{
				OfferCoin: "",
				AskDenom:  "ukrw",
			},
			expectErr: "invalid decimal coin expression",
		},
		{
			name:  "empty ask denom",
			setup: func() {},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000uusd",
				AskDenom:  "",
			},
			expectErr: "invalid ask denom",
		},
		{
			name:  "invalid offer coin format",
			setup: func() {},
			req: &types.QuerySwapRequest{
				OfferCoin: "notacoin",
				AskDenom:  "ukrw",
			},
			expectErr: "invalid decimal coin expression",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			res, err := s.queryClient.Swap(s.ctx, tc.req)
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

func (s *KeeperTestSuite) TestQueryNoahPoolDelta() {
	tests := []struct {
		name  string
		delta math.LegacyDec
	}{
		{
			name:  "zero delta",
			delta: math.LegacyZeroDec(),
		},
		{
			name:  "positive delta",
			delta: math.LegacyNewDec(98765),
		},
		{
			name:  "negative delta",
			delta: math.LegacyNewDec(-12345),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := s.keeper.NoahPoolDelta.Set(s.ctx, tc.delta)
			s.Require().NoError(err)

			res, err := s.queryClient.NoahPoolDelta(s.ctx, &types.QueryNoahPoolDeltaRequest{})
			s.Require().NoError(err)
			s.Require().NotNil(res)
			s.Require().True(tc.delta.Equal(res.NoahPoolDelta))
		})
	}
}
