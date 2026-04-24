package keeper_test

import (
	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	core "noah/types"
	"noah/x/market/types"
	oracletypes "noah/x/oracle/types"
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
		code      codes.Code
		expectErr string
	}{
		{
			name: "empty offer coin",
			req: &types.QuerySwapRequest{
				OfferCoin: "",
				AskDenom:  "ukrw",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid decimal coin expression",
		},
		{
			name: "empty ask denom",
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000uusd",
				AskDenom:  "",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid ask denom",
		},
		{
			name: "invalid offer coin format",
			req: &types.QuerySwapRequest{
				OfferCoin: "notacoin",
				AskDenom:  "ukrw",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid decimal coin expression",
		},
		{
			name: "missing oracle price returns failed precondition",
			setup: func() {
				s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "uusd").
					Return(math.LegacyNewDec(1), nil)
				s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, core.MicroSDRDenom).
					Return(math.LegacyNewDec(1), nil)
				s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, core.MicroSDRDenom).
					Return(math.LegacyNewDec(1), nil)
				s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "unknown").
					Return(math.LegacyZeroDec(), oracletypes.ErrUnknownDenom)
			},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000uusd",
				AskDenom:  "unknown",
			},
			code:      codes.FailedPrecondition,
			expectErr: "no oracle price for denom unknown",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			_, err := s.queryClient.Swap(s.ctx, tc.req)
			s.Require().Error(err)
			s.Require().Equal(tc.code, status.Code(err))
			s.Require().ErrorContains(err, tc.expectErr)
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
