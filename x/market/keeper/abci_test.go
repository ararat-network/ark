package keeper_test

import (
	"cosmossdk.io/math"

	"noah/x/market/types"
)

func (s *KeeperTestSuite) TestEndBlocker_ReplenishPools() {
	tests := []struct {
		name           string
		initialDelta   math.LegacyDec
		recoveryPeriod uint64
		expectedDelta  math.LegacyDec
	}{
		{
			name:           "positive delta decreases",
			initialDelta:   math.LegacyNewDec(1000),
			recoveryPeriod: 10,
			// 1000 - 1000/10 = 900
			expectedDelta: math.LegacyNewDec(900),
		},
		{
			name:           "negative delta increases toward zero",
			initialDelta:   math.LegacyNewDec(-1000),
			recoveryPeriod: 10,
			// -1000 - (-1000/10) = -1000 + 100 = -900
			expectedDelta: math.LegacyNewDec(-900),
		},
		{
			name:           "zero delta stays zero",
			initialDelta:   math.LegacyZeroDec(),
			recoveryPeriod: 10,
			expectedDelta:  math.LegacyZeroDec(),
		},
		{
			name:           "small delta with large recovery period",
			initialDelta:   math.LegacyNewDec(1),
			recoveryPeriod: 100,
			// 1 - 1/100 = 0.99
			expectedDelta: math.LegacyNewDecWithPrec(99, 2),
		},
		{
			name:           "large recovery period — slow convergence",
			initialDelta:   math.LegacyNewDec(14400),
			recoveryPeriod: 14400,
			// 14400 - 14400/14400 = 14399
			expectedDelta: math.LegacyNewDec(14399),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := s.keeper.NoahPoolDelta.Set(s.ctx, tc.initialDelta)
			s.Require().NoError(err)

			err = s.keeper.Params.Set(s.ctx, types.Params{
				BasePool:           math.LegacyNewDec(1000000000000),
				PoolRecoveryPeriod: tc.recoveryPeriod,
				MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
			})
			s.Require().NoError(err)

			err = s.keeper.EndBlocker(s.ctx)
			s.Require().NoError(err)

			delta, err := s.keeper.NoahPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(tc.expectedDelta.Equal(delta), "expected %s, got %s", tc.expectedDelta, delta)
		})
	}
}

func (s *KeeperTestSuite) TestEndBlocker_ConvergesToZero() {
	// Run many iterations — delta should approach zero
	err := s.keeper.NoahPoolDelta.Set(s.ctx, math.LegacyNewDec(100))
	s.Require().NoError(err)

	err = s.keeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(1000000000000),
		PoolRecoveryPeriod: 10,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
	})
	s.Require().NoError(err)

	for i := 0; i < 100; i++ {
		err = s.keeper.EndBlocker(s.ctx)
		s.Require().NoError(err)
	}

	delta, err := s.keeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	// After 100 iterations with factor 0.9: 100 * 0.9^100 ≈ 0.0027
	s.Require().True(delta.Abs().LT(math.LegacyOneDec()))
}
