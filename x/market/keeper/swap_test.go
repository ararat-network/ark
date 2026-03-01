package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/market/types"
)

func (s *KeeperTestSuite) TestComputeOracleRate() {
	// Set oracle rates: uusd=1.0, ukrw=1300.0 (relative to uark)
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "ukrw").
		Return(math.LegacyNewDec(1300), nil).AnyTimes()

	// 1 uusd → 1300 ukrw
	offerCoin := sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1))
	retCoin, err := s.marketKeeper.ComputeOracleRate(s.ctx, offerCoin, "ukrw")
	s.Require().NoError(err)
	s.Require().Equal("ukrw", retCoin.Denom)
	s.Require().True(retCoin.Amount.Equal(math.LegacyNewDec(1300)))

	// Same denom returns same coin
	retCoin, err = s.marketKeeper.ComputeOracleRate(s.ctx, offerCoin, "uusd")
	s.Require().NoError(err)
	s.Require().Equal(offerCoin, retCoin)

	// Unknown denom returns error
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "unknown").
		Return(math.LegacyDec{}, types.ErrNoEffectivePrice).AnyTimes()
	_, err = s.marketKeeper.ComputeOracleRate(s.ctx, offerCoin, "unknown")
	s.Require().Error(err)
	s.Require().ErrorIs(err, types.ErrNoEffectivePrice)
}

func (s *KeeperTestSuite) TestComputeSwap_NoahToNoah() {
	// Noah-to-Noah swap: applies tobin tax, no constant-product spread
	// Set oracle rates for non-ark denoms
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "ukrw").
		Return(math.LegacyNewDec(1300), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	// Set tobin taxes
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "uusd").
		Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes() // 0.25%
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "ukrw").
		Return(math.LegacyNewDecWithPrec(50, 4), nil).AnyTimes() // 0.50%

	// Swap uusd → ukrw
	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000000))
	retCoin, spread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, "ukrw")
	s.Require().NoError(err)
	s.Require().Equal("ukrw", retCoin.Denom)

	// Spread should be max(0.25%, 0.50%) = 0.50%
	s.Require().True(spread.Equal(math.LegacyNewDecWithPrec(50, 4)))

	// Return amount should be oracle-rate converted: 1000000 * 1300 / 1 = 1,300,000,000
	expectedAmount := math.LegacyNewDec(1000000).Mul(math.LegacyNewDec(1300)).Quo(math.LegacyOneDec())
	s.Require().True(retCoin.Amount.Equal(expectedAmount))
}

func (s *KeeperTestSuite) TestComputeSwap_NoahToArk() {
	// Noah-to-Ark swap: applies constant-product spread
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyNewDecWithPrec(5, 1), nil).AnyTimes() // 0.5 SDR per ark
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	// With zero pool delta and default params, spread should equal MinStabilitySpread (2%)
	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000))
	retCoin, spread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
	s.Require().NoError(err)
	s.Require().Equal(core.MicroArkDenom, retCoin.Denom)

	// With balanced pools (delta=0), the CP spread is 0, so minSpread (2%) should apply
	s.Require().True(spread.GTE(math.LegacyNewDecWithPrec(2, 2)))
}

func (s *KeeperTestSuite) TestComputeSwap_ArkToNoah() {
	// Ark-to-Noah swap
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyNewDecWithPrec(5, 1), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	offerCoin := sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))
	retCoin, spread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, "uusd")
	s.Require().NoError(err)
	s.Require().Equal("uusd", retCoin.Denom)
	s.Require().True(spread.GTE(math.LegacyNewDecWithPrec(2, 2)))
}

func (s *KeeperTestSuite) TestComputeSwap_NoahToArk_ConstantProductMath() {
	// Use unit rates (1:1:1) and a small base pool so the CP spread is significant.
	// With balanced pools, CP spread for an offer X into pool P is: X / (X + P).
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	// Small base pool so CP spread is non-trivial
	err := s.marketKeeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(400),
		PoolRecoveryPeriod: 14400,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2), // 2%
	})
	s.Require().NoError(err)

	offerCoin := sdk.NewCoin("uusd", math.NewInt(100))
	retCoin, spread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
	s.Require().NoError(err)

	// Oracle rate with unit rates: 100 uusd = 100 uark
	s.Require().Equal(core.MicroArkDenom, retCoin.Denom)
	s.Require().True(retCoin.Amount.Equal(math.LegacyNewDec(100)))

	// CP spread = offer / (offer + pool) = 100 / (100 + 400) = 0.2
	// 0.2 > minSpread (0.02), so CP spread applies
	expectedSpread := math.LegacyNewDecWithPrec(2, 1) // 0.2
	s.Require().True(spread.Equal(expectedSpread), "expected spread %s, got %s", expectedSpread, spread)
}

func (s *KeeperTestSuite) TestComputeSwap_ArkToNoah_ConstantProductMath() {
	// Symmetric to Noah→Ark with balanced pools: same spread.
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	err := s.marketKeeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(400),
		PoolRecoveryPeriod: 14400,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
	})
	s.Require().NoError(err)

	offerCoin := sdk.NewCoin(core.MicroArkDenom, math.NewInt(100))
	retCoin, spread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, "uusd")
	s.Require().NoError(err)

	// Oracle rate: 100 uark = 100 uusd
	s.Require().Equal("uusd", retCoin.Denom)
	s.Require().True(retCoin.Amount.Equal(math.LegacyNewDec(100)))

	// Balanced pools are symmetric: spread = 100 / 500 = 0.2
	expectedSpread := math.LegacyNewDecWithPrec(2, 1)
	s.Require().True(spread.Equal(expectedSpread), "expected spread %s, got %s", expectedSpread, spread)
}

func (s *KeeperTestSuite) TestComputeSwap_NoahToNoah_OfferTobinTaxHigher() {
	// When the offer denom has a higher tobin tax, that tax should apply.
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "ukrw").
		Return(math.LegacyNewDec(1300), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	// Offer tobin tax (1%) > ask tobin tax (0.25%)
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "uusd").
		Return(math.LegacyNewDecWithPrec(1, 2), nil).AnyTimes() // 1%
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "ukrw").
		Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes() // 0.25%

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000000))
	_, spread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, "ukrw")
	s.Require().NoError(err)

	// Spread = max(1%, 0.25%) = 1%
	s.Require().True(spread.Equal(math.LegacyNewDecWithPrec(1, 2)))
}

func (s *KeeperTestSuite) TestComputeSwap_SpreadNeverBelowMinSpread() {
	// With small offers into the default large pool (1e12), CP spread ≈ 0.
	// The minimum stability spread (2%) should always be the floor.
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	minSpread := math.LegacyNewDecWithPrec(2, 2) // 2%

	for _, amt := range []int64{1, 10, 100, 1000, 10000} {
		offerCoin := sdk.NewCoin("uusd", math.NewInt(amt))
		_, spread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
		s.Require().NoError(err)
		s.Require().True(spread.GTE(minSpread),
			"spread %s below minSpread %s for amount %d", spread, minSpread, amt)
	}
}

func (s *KeeperTestSuite) TestComputeSwap_PoolImbalanceIncreasesSpread() {
	// When pool is imbalanced, spread should increase beyond minSpread
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyNewDecWithPrec(5, 1), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000))

	// Get spread with balanced pool
	_, balancedSpread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
	s.Require().NoError(err)

	// Set large pool delta (imbalanced)
	err = s.marketKeeper.NoahPoolDelta.Set(s.ctx, math.LegacyNewDec(1000000000000))
	s.Require().NoError(err)

	// Spread should be larger with imbalanced pool
	_, imbalancedSpread, err := s.marketKeeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
	s.Require().NoError(err)
	s.Require().True(imbalancedSpread.GT(balancedSpread))
}

func (s *KeeperTestSuite) TestComputeSwap_RecursiveSwap() {
	_, _, err := s.marketKeeper.ComputeSwap(s.ctx, sdk.NewCoin("uusd", math.NewInt(1000)), "uusd")
	s.Require().Error(err)
	s.Require().ErrorIs(err, types.ErrRecursiveSwap)
}

func (s *KeeperTestSuite) TestApplySwapToPool_NoahToArk() {
	// Noah→Ark: pool delta should increase
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000000))
	askCoin := sdk.NewDecCoinFromDec(core.MicroArkDenom, math.LegacyNewDec(2000000))

	err := s.marketKeeper.ApplySwapToPool(s.ctx, offerCoin, askCoin)
	s.Require().NoError(err)

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.IsPositive())
}

func (s *KeeperTestSuite) TestApplySwapToPool_ArkToNoah() {
	// Ark→Noah: pool delta should decrease
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	offerCoin := sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000000))
	askCoin := sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(500000))

	err := s.marketKeeper.ApplySwapToPool(s.ctx, offerCoin, askCoin)
	s.Require().NoError(err)

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.IsNegative())
}

func (s *KeeperTestSuite) TestApplySwapToPool_NoahToArk_ExactDelta() {
	// With unit rates, delta should equal the offer amount exactly.
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000))
	askCoin := sdk.NewDecCoinFromDec(core.MicroArkDenom, math.LegacyNewDec(800))

	err := s.marketKeeper.ApplySwapToPool(s.ctx, offerCoin, askCoin)
	s.Require().NoError(err)

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	// delta = offerCoin → SDR = 1000 * 1.0/1.0 = 1000
	s.Require().True(delta.Equal(math.LegacyNewDec(1000)))
}

func (s *KeeperTestSuite) TestApplySwapToPool_ArkToNoah_ExactDelta() {
	// Delta decreases by the SDR value of the ask coin (not the offer coin).
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	offerCoin := sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))
	askCoin := sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(800))

	err := s.marketKeeper.ApplySwapToPool(s.ctx, offerCoin, askCoin)
	s.Require().NoError(err)

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	// delta = -(askCoin → SDR) = -(800 * 1.0/1.0) = -800
	s.Require().True(delta.Equal(math.LegacyNewDec(-800)))
}

func (s *KeeperTestSuite) TestApplySwapToPool_NoahToNoah() {
	// Noah→Noah: pool delta should not change
	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000000))
	askCoin := sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(1300000000))

	err := s.marketKeeper.ApplySwapToPool(s.ctx, offerCoin, askCoin)
	s.Require().NoError(err)

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.IsZero())
}
