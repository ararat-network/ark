package keeper_test

import (
	"math/big"

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
				AskDenom:  "akrw",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid decimal coin expression",
		},
		{
			name: "empty ask denom",
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000ausd",
				AskDenom:  "",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid ask denom",
		},
		{
			name: "invalid offer coin format",
			req: &types.QuerySwapRequest{
				OfferCoin: "notacoin",
				AskDenom:  "akrw",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid decimal coin expression",
		},
		{
			name: "missing oracle price returns failed precondition",
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "ausd", "unknown").
					Return(nil, oracletypes.ErrUnknownDenom)
			},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000ausd",
				AskDenom:  "unknown",
			},
			code:      codes.FailedPrecondition,
			expectErr: "no price registered with oracle",
		},
		{
			name: "stale oracle price returns failed precondition",
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "ausd", "akrw").
					Return(nil, oracletypes.ErrStaleExchangeRate)
			},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000ausd",
				AskDenom:  "akrw",
			},
			code:      codes.FailedPrecondition,
			expectErr: "stale exchange rate",
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

func (s *KeeperTestSuite) TestQuerySwapAcceptsLargeRepresentableAmount() {
	largeAmount := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 200))
	offerCoin := sdk.NewCoin("ausd", largeAmount)

	s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSnapshot{
			"ausd":             math.LegacyOneDec(),
			chain.SDRBaseDenom: math.LegacyOneDec(),
			"akrw":             math.LegacyOneDec(),
		}, nil)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: "ausd", TobinTax: math.LegacyZeroDec()},
		{Denom: "akrw", TobinTax: math.LegacyZeroDec()},
	}, nil)

	res, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  "akrw",
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoin("akrw", largeAmount), res.SwapCoin)
}

func (s *KeeperTestSuite) TestQuerySwapDirectStableConversionAvoidsUnrepresentablePoolUnitIntermediate() {
	offerAmount := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	offerCoin := sdk.NewCoin("ausd", offerAmount)

	s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSnapshot{
			"ausd":             math.LegacyOneDec(),
			chain.SDRBaseDenom: math.LegacyNewDec(2),
			"akrw":             math.LegacyOneDec(),
		}, nil)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: "ausd", TobinTax: math.LegacyZeroDec()},
		{Denom: "akrw", TobinTax: math.LegacyZeroDec()},
	}, nil)

	res, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  "akrw",
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoin("akrw", offerAmount), res.SwapCoin)
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
			expectedSwapCoin: sdk.NewCoin("akrw", math.NewInt(100)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("akrw", math.LegacyMustNewDecFromStr("0.5")),
		},
		{
			name:             "positive spread deducts explicit fee",
			offerRate:        math.LegacyOneDec(),
			askRate:          math.LegacyNewDec(100),
			tobinTax:         math.LegacyMustNewDecFromStr("0.2"),
			expectedSwapCoin: sdk.NewCoin("akrw", math.NewInt(80)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("akrw", math.LegacyNewDec(20)),
		},
		{
			name:             "truncation remainder is folded into fee",
			offerRate:        math.LegacyNewDec(20),
			askRate:          math.LegacyNewDec(2011),
			tobinTax:         math.LegacyMustNewDecFromStr("0.1"),
			expectedSwapCoin: sdk.NewCoin("akrw", math.NewInt(90)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("akrw", math.LegacyMustNewDecFromStr("10.55")),
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
				OfferCoin: "1ausd",
				AskDenom:  "akrw",
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
			s.Require().Equal(chain.SDRBaseDenom, res.PoolDenom)
		})
	}
}

func (s *KeeperTestSuite) setupQuerySwapMocks(offerRate math.LegacyDec, askRate math.LegacyDec, tobinTax math.LegacyDec) {
	s.oracleKeeper.EXPECT().GetRateSnapshot(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSnapshot{
			"ausd":             offerRate,
			chain.SDRBaseDenom: math.LegacyOneDec(),
			"akrw":             askRate,
		}, nil)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: "ausd", TobinTax: tobinTax},
		{Denom: "akrw", TobinTax: tobinTax},
	}, nil)
}
