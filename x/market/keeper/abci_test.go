package keeper_test

import (
	"noah/x/market/types"

	"cosmossdk.io/math"
)

func (s *KeeperTestSuite) TestEndBlocker_ReplenishPools() {
	// Set known pool delta and recovery period
	initialDelta := math.LegacyNewDec(1000)
	err := s.marketKeeper.NoahPoolDelta.Set(s.ctx, initialDelta)
	s.Require().NoError(err)

	recoveryPeriod := uint64(10)
	err = s.marketKeeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(1000000000000),
		PoolRecoveryPeriod: recoveryPeriod,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
	})
	s.Require().NoError(err)

	// After one EndBlocker, delta should decrease by delta/recoveryPeriod
	err = s.marketKeeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)

	// Expected: 1000 - 1000/10 = 900
	expectedDelta := initialDelta.Sub(initialDelta.QuoInt64(int64(recoveryPeriod)))
	s.Require().True(delta.Equal(expectedDelta))
}

func (s *KeeperTestSuite) TestEndBlocker_ConvergesToZero() {
	// Set small pool delta
	err := s.marketKeeper.NoahPoolDelta.Set(s.ctx, math.LegacyNewDec(100))
	s.Require().NoError(err)

	err = s.marketKeeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(1000000000000),
		PoolRecoveryPeriod: 10,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
	})
	s.Require().NoError(err)

	// Run multiple EndBlockers — delta should approach zero
	for i := 0; i < 100; i++ {
		err = s.marketKeeper.EndBlocker(s.ctx)
		s.Require().NoError(err)
	}

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	// After 100 iterations with factor 0.9: 100 * 0.9^100 ≈ 0.0027
	// Delta should be significantly smaller than the initial value
	s.Require().True(delta.Abs().LT(math.LegacyOneDec()))
}

func (s *KeeperTestSuite) TestEndBlocker_NegativeDelta() {
	// Negative delta should also converge toward zero
	err := s.marketKeeper.NoahPoolDelta.Set(s.ctx, math.LegacyNewDec(-1000))
	s.Require().NoError(err)

	err = s.marketKeeper.Params.Set(s.ctx, types.Params{
		BasePool:           math.LegacyNewDec(1000000000000),
		PoolRecoveryPeriod: 10,
		MinStabilitySpread: math.LegacyNewDecWithPrec(2, 2),
	})
	s.Require().NoError(err)

	err = s.marketKeeper.EndBlocker(s.ctx)
	s.Require().NoError(err)

	delta, err := s.marketKeeper.NoahPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	// -1000 - (-1000/10) = -1000 + 100 = -900
	s.Require().True(delta.Equal(math.LegacyNewDec(-900)))
}
