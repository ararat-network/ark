package keeper_test

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
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
				s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, chain.MicroSDRDenom).
					Return(math.LegacyNewDec(1), nil)
				s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, chain.MicroSDRDenom).
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

func (s *KeeperTestSuite) TestQuerySwapOutcome() {
	tests := []struct {
		name             string
		offerRate        math.LegacyDec
		askRate          math.LegacyDec
		tobinTax         math.LegacyDec
		expectedSwapCoin sdk.Coin
		expectedSwapFee  sdk.DecCoin
		code             codes.Code
		expectErr        string
	}{
		{
			name:             "zero spread returns truncation remainder as fee",
			offerRate:        math.LegacyNewDec(2),
			askRate:          math.LegacyNewDec(201),
			tobinTax:         math.LegacyZeroDec(),
			expectedSwapCoin: sdk.NewCoin("ukrw", math.NewInt(100)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("0.5")),
		},
		{
			name:             "positive spread deducts explicit fee",
			offerRate:        math.LegacyOneDec(),
			askRate:          math.LegacyNewDec(100),
			tobinTax:         math.LegacyMustNewDecFromStr("0.2"),
			expectedSwapCoin: sdk.NewCoin("ukrw", math.NewInt(80)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(20)),
		},
		{
			name:             "truncation remainder is folded into fee",
			offerRate:        math.LegacyNewDec(20),
			askRate:          math.LegacyNewDec(2011),
			tobinTax:         math.LegacyMustNewDecFromStr("0.1"),
			expectedSwapCoin: sdk.NewCoin("ukrw", math.NewInt(90)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("ukrw", math.LegacyMustNewDecFromStr("10.55")),
		},
		{
			name:      "zero swap coin returns invalid argument",
			offerRate: math.LegacyNewDec(2),
			askRate:   math.LegacyOneDec(),
			tobinTax:  math.LegacyZeroDec(),
			code:      codes.InvalidArgument,
			expectErr: "zero swap coin",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.setupQuerySwapMocks(tc.offerRate, tc.askRate, tc.tobinTax)

			res, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
				OfferCoin: "1uusd",
				AskDenom:  "ukrw",
			})
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}

			s.Require().NoError(err)
			s.Require().NotNil(res)
			s.Require().Equal(tc.expectedSwapCoin, res.SwapCoin)
			s.Require().Equal(tc.expectedSwapFee.Denom, res.SwapFee.Denom)
			s.Require().True(tc.expectedSwapFee.Amount.Equal(res.SwapFee.Amount))
		})
	}
}

func (s *KeeperTestSuite) TestQueryArkPoolDelta() {
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
			err := s.keeper.ArkPoolDelta.Set(s.ctx, tc.delta)
			s.Require().NoError(err)

			res, err := s.queryClient.ArkPoolDelta(s.ctx, &types.QueryArkPoolDeltaRequest{})
			s.Require().NoError(err)
			s.Require().NotNil(res)
			s.Require().True(tc.delta.Equal(res.ArkPoolDelta))
		})
	}
}

func (s *KeeperTestSuite) setupQuerySwapMocks(offerRate math.LegacyDec, askRate math.LegacyDec, tobinTax math.LegacyDec) {
	s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "uusd").
		Return(offerRate, nil)
	s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, chain.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).Times(2)
	s.oracleKeeper.EXPECT().GetExchangeRate(s.ctx, "ukrw").
		Return(askRate, nil)
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, "uusd").
		Return(tobinTax, nil)
	s.oracleKeeper.EXPECT().GetTobinTax(s.ctx, "ukrw").
		Return(tobinTax, nil)
}
