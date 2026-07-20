package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestSwapQuote_RecursiveSwap() {
	_, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: "1000uusd",
		AskDenom:  "uusd",
	})
	s.Require().Error(err)
	s.Require().ErrorContains(err, types.ErrRecursiveSwap.Error())
}

func (s *KeeperTestSuite) TestSwapQuote_ArkToArk_TobinTax() {
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), "uusd", "ukrw").
		Return(oracletypes.RateSnapshot{
			"uusd": math.LegacyOneDec(),
			"ukrw": math.LegacyNewDec(1300),
		}, nil).AnyTimes()

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000000))
	tests := []struct {
		name           string
		offerTobinTax  math.LegacyDec
		askTobinTax    math.LegacyDec
		expectedSpread math.LegacyDec
	}{
		{
			name:           "equal tobin tax uses either rate",
			offerTobinTax:  math.LegacyNewDecWithPrec(25, 4), // 0.25%
			askTobinTax:    math.LegacyNewDecWithPrec(25, 4), // 0.25%
			expectedSpread: math.LegacyNewDecWithPrec(25, 4), // 0.25%
		},
		{
			name:           "offer tobin tax higher uses offer rate",
			offerTobinTax:  math.LegacyNewDecWithPrec(1, 2),  // 1%
			askTobinTax:    math.LegacyNewDecWithPrec(25, 4), // 0.25%
			expectedSpread: math.LegacyNewDecWithPrec(1, 2),  // 1%
		},
		{
			name:           "ask tobin tax higher uses ask rate",
			offerTobinTax:  math.LegacyNewDecWithPrec(25, 4), // 0.25%
			askTobinTax:    math.LegacyNewDecWithPrec(50, 4), // 0.50%
			expectedSpread: math.LegacyNewDecWithPrec(50, 4), // 0.50%
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "uusd").
				Return(tc.offerTobinTax, nil)
			s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "ukrw").
				Return(tc.askTobinTax, nil)

			response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
				OfferCoin: offerCoin.String(),
				AskDenom:  "ukrw",
			})
			s.Require().NoError(err)
			s.Require().Equal("ukrw", response.SwapCoin.Denom)
			expectedGross := math.LegacyNewDec(1_300_000_000)
			expectedFee := tc.expectedSpread.Mul(expectedGross)
			s.Require().True(expectedFee.Equal(response.SwapFee.Amount),
				"expected fee %s, got %s", expectedFee, response.SwapFee.Amount)
		})
	}
}

func (s *KeeperTestSuite) TestSwapQuote_ConstantProduct() {
	// Unit rates (1:1:1) with a small base pool so CP spread is significant.
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(oracletypes.RateSnapshot{
			"uusd":               math.LegacyOneDec(),
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroSDRDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()

	err := s.keeper.Params.Set(s.ctx, types.Params{
		BasePool:           sdrBasePool(math.LegacyNewDec(400)),
		PoolRecoveryPeriod: 14400,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2), // 2%
	})
	s.Require().NoError(err)

	tests := []struct {
		name           string
		offerCoin      sdk.Coin
		askDenom       string
		expectedDenom  string
		expectedAmount math.LegacyDec
		expectedSpread math.LegacyDec
	}{
		{
			name:           "ark to noah — CP spread = 100/500 = 0.2",
			offerCoin:      sdk.NewCoin("uusd", math.NewInt(100)),
			askDenom:       chain.MicroNoahDenom,
			expectedDenom:  chain.MicroNoahDenom,
			expectedAmount: math.LegacyNewDec(100),
			expectedSpread: math.LegacyNewDecWithPrec(2, 1), // 0.2
		},
		{
			name:           "noah to ark — symmetric with balanced pools",
			offerCoin:      sdk.NewCoin(chain.MicroNoahDenom, math.NewInt(100)),
			askDenom:       "uusd",
			expectedDenom:  "uusd",
			expectedAmount: math.LegacyNewDec(100),
			expectedSpread: math.LegacyNewDecWithPrec(2, 1), // 0.2
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
				OfferCoin: tc.offerCoin.String(),
				AskDenom:  tc.askDenom,
			})
			s.Require().NoError(err)
			s.Require().Equal(tc.expectedDenom, response.SwapCoin.Denom)
			expectedFee := tc.expectedSpread.Mul(tc.expectedAmount)
			s.Require().True(expectedFee.Equal(response.SwapFee.Amount),
				"expected fee %s, got %s", expectedFee, response.SwapFee.Amount)
			grossAmount := math.LegacyNewDecFromInt(response.SwapCoin.Amount).Add(response.SwapFee.Amount)
			s.Require().True(tc.expectedAmount.Equal(grossAmount),
				"expected gross amount %s, got %s", tc.expectedAmount, grossAmount)
		})
	}
}

func (s *KeeperTestSuite) TestSwapQuote_SpreadNeverBelowMinSpread() {
	// With small offers into the default large pool (1e12), CP spread ≈ 0.
	// The minimum stability spread (2%) should always be the floor.
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), "uusd", chain.MicroSDRDenom, chain.MicroNoahDenom).
		Return(oracletypes.RateSnapshot{
			"uusd":               math.LegacyOneDec(),
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroSDRDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()

	minSpread := math.LegacyNewDecWithPrec(2, 2) // 2%

	for _, amt := range []int64{10, 100, 1000, 10000} {
		offerCoin := sdk.NewCoin("uusd", math.NewInt(amt))
		response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
			OfferCoin: offerCoin.String(),
			AskDenom:  chain.MicroNoahDenom,
		})
		s.Require().NoError(err)
		spread := response.SwapFee.Amount.Quo(math.LegacyNewDec(amt))
		s.Require().True(spread.GTE(minSpread),
			"spread %s below minSpread %s for amount %d", spread, minSpread, amt)
	}
}

func (s *KeeperTestSuite) TestSwapQuote_NegativeRawSpreadUsesMinimumSpread() {
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		"uusd",
		chain.MicroSDRDenom,
		chain.MicroNoahDenom,
	).Return(oracletypes.RateSnapshot{
		"uusd":               math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacyOneDec(),
		chain.MicroNoahDenom: math.LegacyOneDec(),
	}, nil)
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(
		s.ctx,
		math.LegacyMustNewDecFromStr("-500000000000"),
	))

	offerCoin := sdk.NewInt64Coin("uusd", 100_000_000_000)
	response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  chain.MicroNoahDenom,
	})
	s.Require().NoError(err)
	spread := response.SwapFee.Amount.Quo(math.LegacyNewDecFromInt(offerCoin.Amount))
	s.Require().True(types.DefaultMinStabilitySpread.Equal(spread))
}

func (s *KeeperTestSuite) TestSwapQuote_ExtremeNegativeRawSpreadUsesMinimumWithoutOverflow() {
	basePool := math.LegacyMustNewDecFromStr("100000000000000000000000000000")
	arkPoolDelta := basePool.Neg().Add(math.LegacySmallestDec())
	_, err := types.NewEffectivePools(basePool, arkPoolDelta)
	s.Require().NoError(err)

	params := types.DefaultParams()
	params.BasePool = sdrBasePool(basePool)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, arkPoolDelta))

	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroUSDDenom,
		chain.MicroSDRDenom,
		chain.MicroNoahDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroUSDDenom:  math.LegacyOneDec(),
		chain.MicroSDRDenom:  math.LegacySmallestDec(),
		chain.MicroNoahDenom: math.LegacyOneDec(),
	}, nil)

	response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: sdk.NewInt64Coin(chain.MicroUSDDenom, 1_000_000).String(),
		AskDenom:  chain.MicroNoahDenom,
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.MicroNoahDenom, 980_000), response.SwapCoin)
	s.Require().True(math.LegacyNewDec(20_000).Equal(response.SwapFee.Amount))
}

func (s *KeeperTestSuite) TestSwapQuote_PoolImbalanceIncreasesSpread() {
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), "uusd", chain.MicroSDRDenom, chain.MicroNoahDenom).
		Return(oracletypes.RateSnapshot{
			"uusd":               math.LegacyOneDec(),
			chain.MicroNoahDenom: math.LegacyNewDecWithPrec(5, 1),
			chain.MicroSDRDenom:  math.LegacyNewDecWithPrec(17, 1),
		}, nil).AnyTimes()

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000))

	// Get spread with balanced pool
	balancedQuote, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  chain.MicroNoahDenom,
	})
	s.Require().NoError(err)

	// Set large pool delta (imbalanced)
	err = s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyNewDec(1000000000000))
	s.Require().NoError(err)

	// Spread should be larger with imbalanced pool
	imbalancedQuote, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  chain.MicroNoahDenom,
	})
	s.Require().NoError(err)
	s.Require().True(imbalancedQuote.SwapFee.Amount.GT(balancedQuote.SwapFee.Amount))
}
