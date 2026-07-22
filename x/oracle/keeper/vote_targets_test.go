package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestGetVoteTargets() {
	expected := []string{chain.MicroKRWDenom, chain.MicroUSDDenom}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargets{
		Denoms:  expected,
		Version: types.InitialVoteTargetVersion,
	}))

	voteTargetSet, err := s.keeper.GetVoteTargets(s.ctx, 10)
	s.Require().NoError(err)
	s.Require().Equal(types.InitialVoteTargetVersion, voteTargetSet.Version)
	s.Require().Equal(expected, voteTargetSet.Denoms)
}

func (s *KeeperTestSuite) TestScheduleVoteTargets() {
	testCases := []struct {
		name      string
		denoms    []string
		expectErr string
	}{
		{
			name:   "active targets are a no-op",
			denoms: []string{chain.MicroUSDDenom},
		},
		{
			name:      "invalid targets are rejected",
			denoms:    []string{"u"},
			expectErr: "pending vote targets denom must be a micro denom",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			expected := types.VoteTargets{
				Denoms:  []string{chain.MicroUSDDenom},
				Version: types.InitialVoteTargetVersion,
			}
			s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, expected))

			err := s.keeper.ScheduleVoteTargets(s.ctx, tc.denoms)
			if tc.expectErr == "" {
				s.Require().NoError(err)
			} else {
				s.Require().ErrorContains(err, tc.expectErr)
			}

			actual, err := s.keeper.VoteTargets.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(expected, actual)
		})
	}
}

func (s *KeeperTestSuite) TestVoteTargetTransition() {
	oldVoteTargets := []string{chain.MicroKRWDenom, chain.MicroUSDDenom}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargets{
		Denoms:  oldVoteTargets,
		Version: types.InitialVoteTargetVersion,
	}))
	for _, denom := range oldVoteTargets {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, denom, newStoredExchangeRate(denom, math.LegacyOneDec())))
	}

	nextVoteTargets := []string{chain.MicroUSDDenom, chain.MicroSDRDenom}
	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.VoteTargetActivationDelayBlocks
	s.Require().NoError(s.keeper.ScheduleVoteTargets(s.ctx, nextVoteTargets))
	s.Require().Equal([]string{chain.MicroUSDDenom, chain.MicroSDRDenom}, nextVoteTargets)
	s.Require().NoError(s.keeper.ScheduleVoteTargets(s.ctx, nextVoteTargets))
	s.Require().ErrorContains(
		s.keeper.ScheduleVoteTargets(s.ctx, oldVoteTargets),
		"already pending activation",
	)

	before, err := s.keeper.GetVoteTargets(s.ctx, activationHeight-1)
	s.Require().NoError(err)
	s.Require().Equal(types.InitialVoteTargetVersion, before.Version)
	s.Require().Equal(oldVoteTargets, before.Denoms)

	atActivation, err := s.keeper.GetVoteTargets(s.ctx, activationHeight)
	s.Require().NoError(err)
	s.Require().Equal(types.InitialVoteTargetVersion+1, atActivation.Version)
	s.Require().Equal([]string{chain.MicroSDRDenom, chain.MicroUSDDenom}, atActivation.Denoms)

	beforeActivationCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(activationHeight - 1)
	s.Require().NoError(s.keeper.AdvanceVoteTargets(beforeActivationCtx))
	hasKRWExchangeRate, err := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroKRWDenom)
	s.Require().NoError(err)
	s.Require().True(hasKRWExchangeRate)

	activationCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(activationHeight)
	s.Require().NoError(s.keeper.AdvanceVoteTargets(activationCtx))
	state, err := s.keeper.VoteTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Nil(state.Pending)
	s.Require().Equal(types.InitialVoteTargetVersion+1, state.Version)
	s.Require().Equal([]string{chain.MicroSDRDenom, chain.MicroUSDDenom}, state.Denoms)

	hasUSDExchangeRate, err := s.keeper.ExchangeRate.Has(s.ctx, chain.MicroUSDDenom)
	s.Require().NoError(err)
	s.Require().True(hasUSDExchangeRate)

	hasKRWExchangeRate, err = s.keeper.ExchangeRate.Has(s.ctx, chain.MicroKRWDenom)
	s.Require().NoError(err)
	s.Require().False(hasKRWExchangeRate)
}

func (s *KeeperTestSuite) TestEmptyVoteTargetTransition() {
	oldVoteTargets := []string{chain.MicroKRWDenom, chain.MicroUSDDenom}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargets{
		Denoms:  oldVoteTargets,
		Version: types.InitialVoteTargetVersion,
	}))
	for _, denom := range oldVoteTargets {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, denom, newStoredExchangeRate(denom, math.LegacyOneDec())))
	}

	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.VoteTargetActivationDelayBlocks
	s.Require().NoError(s.keeper.ScheduleVoteTargets(s.ctx, []string{}))

	atActivation, err := s.keeper.GetVoteTargets(s.ctx, activationHeight)
	s.Require().NoError(err)
	s.Require().Equal(types.InitialVoteTargetVersion+1, atActivation.Version)
	s.Require().Empty(atActivation.Denoms)

	activationCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(activationHeight)
	s.Require().NoError(s.keeper.AdvanceVoteTargets(activationCtx))
	state, err := s.keeper.VoteTargets.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Nil(state.Pending)
	s.Require().Equal(types.InitialVoteTargetVersion+1, state.Version)
	s.Require().Empty(state.Denoms)

	for _, denom := range oldVoteTargets {
		hasExchangeRate, err := s.keeper.ExchangeRate.Has(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().False(hasExchangeRate)
	}
}
