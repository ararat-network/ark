package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/market/types"
)

func (s *KeeperTestSuite) TestApplySwapToPool() {
	// Unit rates so SDR conversion is 1:1
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	tests := []struct {
		name          string
		offerCoin     sdk.Coin
		askCoin       sdk.DecCoin
		expectedDelta math.LegacyDec
	}{
		{
			name:      "noah to ark — delta increases by offer SDR value",
			offerCoin: sdk.NewCoin("uusd", math.NewInt(1000)),
			askCoin:   sdk.NewDecCoinFromDec(core.MicroArkDenom, math.LegacyNewDec(800)),
			// delta = offerCoin in SDR = 1000
			expectedDelta: math.LegacyNewDec(1000),
		},
		{
			name:      "ark to noah — delta decreases by ask SDR value",
			offerCoin: sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000)),
			askCoin:   sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(800)),
			// delta = -(askCoin in SDR) = -800
			expectedDelta: math.LegacyNewDec(-800),
		},
		{
			name:          "noah to noah — no delta change",
			offerCoin:     sdk.NewCoin("uusd", math.NewInt(1000000)),
			askCoin:       sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(1300000000)),
			expectedDelta: math.LegacyZeroDec(),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Reset pool delta
			s.Require().NoError(s.keeper.NoahPoolDelta.Set(s.ctx, math.LegacyZeroDec()))

			err := s.keeper.ApplySwapToPool(s.ctx, tc.offerCoin, tc.askCoin)
			s.Require().NoError(err)

			delta, err := s.keeper.NoahPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(tc.expectedDelta.Equal(delta),
				"expected delta %s, got %s", tc.expectedDelta, delta)
		})
	}
}

func (s *KeeperTestSuite) TestComputeSwap_RecursiveSwap() {
	_, _, err := s.keeper.ComputeSwap(s.ctx, sdk.NewCoin("uusd", math.NewInt(1000)), "uusd")
	s.Require().Error(err)
	s.Require().ErrorIs(err, types.ErrRecursiveSwap)
}

func (s *KeeperTestSuite) TestComputeSwap_ConstantProduct() {
	// Unit rates (1:1:1) with a small base pool so CP spread is significant.
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	err := s.keeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(400),
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
			name:           "noah to ark — CP spread = 100/500 = 0.2",
			offerCoin:      sdk.NewCoin("uusd", math.NewInt(100)),
			askDenom:       core.MicroArkDenom,
			expectedDenom:  core.MicroArkDenom,
			expectedAmount: math.LegacyNewDec(100),
			expectedSpread: math.LegacyNewDecWithPrec(2, 1), // 0.2
		},
		{
			name:           "ark to noah — symmetric with balanced pools",
			offerCoin:      sdk.NewCoin(core.MicroArkDenom, math.NewInt(100)),
			askDenom:       "uusd",
			expectedDenom:  "uusd",
			expectedAmount: math.LegacyNewDec(100),
			expectedSpread: math.LegacyNewDecWithPrec(2, 1), // 0.2
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			retCoin, spread, err := s.keeper.ComputeSwap(s.ctx, tc.offerCoin, tc.askDenom)
			s.Require().NoError(err)
			s.Require().Equal(tc.expectedDenom, retCoin.Denom)
			s.Require().True(tc.expectedAmount.Equal(retCoin.Amount),
				"expected amount %s, got %s", tc.expectedAmount, retCoin.Amount)
			s.Require().True(tc.expectedSpread.Equal(spread),
				"expected spread %s, got %s", tc.expectedSpread, spread)
		})
	}
}

func (s *KeeperTestSuite) TestComputeSwap_SpreadNeverBelowMinSpread() {
	// With small offers into the default large pool (1e12), CP spread ≈ 0.
	// The minimum stability spread (2%) should always be the floor.
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyOneDec(), nil).AnyTimes()

	minSpread := math.LegacyNewDecWithPrec(2, 2) // 2%

	for _, amt := range []int64{1, 10, 100, 1000, 10000} {
		offerCoin := sdk.NewCoin("uusd", math.NewInt(amt))
		_, spread, err := s.keeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
		s.Require().NoError(err)
		s.Require().True(spread.GTE(minSpread),
			"spread %s below minSpread %s for amount %d", spread, minSpread, amt)
	}
}

func (s *KeeperTestSuite) TestComputeSwap_PoolImbalanceIncreasesSpread() {
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), core.MicroArkDenom).
		Return(math.LegacyNewDecWithPrec(5, 1), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000))

	// Get spread with balanced pool
	_, balancedSpread, err := s.keeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
	s.Require().NoError(err)

	// Set large pool delta (imbalanced)
	err = s.keeper.NoahPoolDelta.Set(s.ctx, math.LegacyNewDec(1000000000000))
	s.Require().NoError(err)

	// Spread should be larger with imbalanced pool
	_, imbalancedSpread, err := s.keeper.ComputeSwap(s.ctx, offerCoin, core.MicroArkDenom)
	s.Require().NoError(err)
	s.Require().True(imbalancedSpread.GT(balancedSpread))
}

func (s *KeeperTestSuite) TestComputeOracleRate() {
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), "ukrw").
		Return(math.LegacyNewDec(1300), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetExchangeRate(gomock.Any(), "unknown").
		Return(math.LegacyDec{}, types.ErrNoEffectivePrice).AnyTimes()

	tests := []struct {
		name      string
		offerCoin sdk.DecCoin
		askDenom  string
		expected  sdk.DecCoin
		expectErr bool
	}{
		{
			name:      "uusd to ukrw",
			offerCoin: sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1)),
			askDenom:  "ukrw",
			expected:  sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(1300)),
		},
		{
			name:      "ukrw to uusd",
			offerCoin: sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(1300)),
			askDenom:  "uusd",
			expected:  sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1)),
		},
		{
			name:      "same denom returns same coin",
			offerCoin: sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1)),
			askDenom:  "uusd",
			expected:  sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1)),
		},
		{
			name:      "unknown ask denom",
			offerCoin: sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(1)),
			askDenom:  "unknown",
			expectErr: true,
		},
		{
			name:      "unknown offer denom",
			offerCoin: sdk.NewDecCoinFromDec("unknown", math.LegacyNewDec(1)),
			askDenom:  "uusd",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			result, err := s.keeper.ComputeOracleRate(s.ctx, tc.offerCoin, tc.askDenom)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().ErrorIs(err, types.ErrNoEffectivePrice)
			} else {
				s.Require().NoError(err)
				s.Require().Equal(tc.expected.Denom, result.Denom)
				s.Require().True(tc.expected.Amount.Equal(result.Amount),
					"expected %s, got %s", tc.expected.Amount, result.Amount)
			}
		})
	}
}
