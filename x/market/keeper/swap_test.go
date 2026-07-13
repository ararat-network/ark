package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestComputeSwap_RecursiveSwap() {
	_, _, err := s.keeper.ComputeSwap(s.ctx, sdk.NewCoin("uusd", math.NewInt(1000)), "uusd")
	s.Require().Error(err)
	s.Require().ErrorIs(err, types.ErrRecursiveSwap)
}

func (s *KeeperTestSuite) TestComputeSwap_ArkToArk_TobinTax() {
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), "uusd", chain.MicroSDRDenom, "ukrw").
		Return(oracletypes.RateSnapshot{
			"uusd":              math.LegacyOneDec(),
			"ukrw":              math.LegacyNewDec(1300),
			chain.MicroSDRDenom: math.LegacyNewDecWithPrec(17, 1),
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

			retCoin, spread, err := s.keeper.ComputeSwap(s.ctx, offerCoin, "ukrw")
			s.Require().NoError(err)
			s.Require().Equal("ukrw", retCoin.Denom)
			s.Require().True(tc.expectedSpread.Equal(spread),
				"expected spread %s, got %s", tc.expectedSpread, spread)
		})
	}
}

func (s *KeeperTestSuite) TestComputeSwap_ConstantProduct() {
	// Unit rates (1:1:1) with a small base pool so CP spread is significant.
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(oracletypes.RateSnapshot{
			"uusd":               math.LegacyOneDec(),
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroSDRDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()

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
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), "uusd", chain.MicroSDRDenom, chain.MicroNoahDenom).
		Return(oracletypes.RateSnapshot{
			"uusd":               math.LegacyOneDec(),
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroSDRDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()

	minSpread := math.LegacyNewDecWithPrec(2, 2) // 2%

	for _, amt := range []int64{1, 10, 100, 1000, 10000} {
		offerCoin := sdk.NewCoin("uusd", math.NewInt(amt))
		_, spread, err := s.keeper.ComputeSwap(s.ctx, offerCoin, chain.MicroNoahDenom)
		s.Require().NoError(err)
		s.Require().True(spread.GTE(minSpread),
			"spread %s below minSpread %s for amount %d", spread, minSpread, amt)
	}
}

func (s *KeeperTestSuite) TestComputeSwap_PoolImbalanceIncreasesSpread() {
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), "uusd", chain.MicroSDRDenom, chain.MicroNoahDenom).
		Return(oracletypes.RateSnapshot{
			"uusd":               math.LegacyOneDec(),
			chain.MicroNoahDenom: math.LegacyNewDecWithPrec(5, 1),
			chain.MicroSDRDenom:  math.LegacyNewDecWithPrec(17, 1),
		}, nil).AnyTimes()

	offerCoin := sdk.NewCoin("uusd", math.NewInt(1000))

	// Get spread with balanced pool
	_, balancedSpread, err := s.keeper.ComputeSwap(s.ctx, offerCoin, chain.MicroNoahDenom)
	s.Require().NoError(err)

	// Set large pool delta (imbalanced)
	err = s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyNewDec(1000000000000))
	s.Require().NoError(err)

	// Spread should be larger with imbalanced pool
	_, imbalancedSpread, err := s.keeper.ComputeSwap(s.ctx, offerCoin, chain.MicroNoahDenom)
	s.Require().NoError(err)
	s.Require().True(imbalancedSpread.GT(balancedSpread))
}
