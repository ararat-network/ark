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
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
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

func (s *KeeperTestSuite) TestComputeOracleRate() {
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "ukrw").
		Return(math.LegacyNewDec(1300), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "unknown").
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
