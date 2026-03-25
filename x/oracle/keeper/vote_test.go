package keeper_test

import (
	"errors"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestPickReferenceDenom() {
	tests := []struct {
		name          string
		setup         func()
		voteTargets   map[string]math.LegacyDec
		voteMap       map[string]types.ExchangeRateBallot
		expectedDenom string
		prunedTargets []string
		prunedVotes   []string
	}{
		{
			name: "single denom above threshold",
			setup: func() {
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			voteMap: map[string]types.ExchangeRateBallot{
				core.MicroKRWDenom: {
					types.NewVoteForTally(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
					types.NewVoteForTally(math.LegacyNewDec(1001), core.MicroKRWDenom, valAddr2, 10),
				},
			},
			expectedDenom: core.MicroKRWDenom,
		},
		{
			name: "higher turnout wins",
			setup: func() {
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			voteMap: map[string]types.ExchangeRateBallot{
				core.MicroKRWDenom: {
					types.NewVoteForTally(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
					types.NewVoteForTally(math.LegacyNewDec(1001), core.MicroKRWDenom, valAddr2, 10),
				},
				core.MicroUSDDenom: {
					types.NewVoteForTally(math.LegacyNewDec(1), core.MicroUSDDenom, valAddr1, 10),
				},
			},
			expectedDenom: core.MicroKRWDenom,
		},
		{
			name: "same turnout chooses alphabetical denom",
			setup: func() {
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
				core.MicroUSDDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			voteMap: map[string]types.ExchangeRateBallot{
				core.MicroKRWDenom: {
					types.NewVoteForTally(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
					types.NewVoteForTally(math.LegacyNewDec(1001), core.MicroKRWDenom, valAddr2, 10),
				},
				core.MicroUSDDenom: {
					types.NewVoteForTally(math.LegacyNewDec(1), core.MicroUSDDenom, valAddr1, 10),
					types.NewVoteForTally(math.LegacyNewDec(2), core.MicroUSDDenom, valAddr2, 10),
				},
			},
			expectedDenom: core.MicroKRWDenom,
		},
		{
			name: "below threshold gets pruned",
			setup: func() {
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			voteMap: map[string]types.ExchangeRateBallot{
				core.MicroKRWDenom: {
					types.NewVoteForTally(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 1),
				},
			},
			prunedTargets: []string{core.MicroKRWDenom},
			prunedVotes:   []string{core.MicroKRWDenom},
		},
		{
			name: "denom missing from vote targets pruned from vote map",
			setup: func() {
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			voteTargets: map[string]math.LegacyDec{},
			voteMap: map[string]types.ExchangeRateBallot{
				"ufoo": {
					types.NewVoteForTally(math.LegacyNewDec(1), "ufoo", valAddr1, 10),
				},
			},
			prunedVotes: []string{"ufoo"},
		},
		{
			name: "no votes returns empty denom",
			setup: func() {
				s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDecWithPrec(25, 4),
			},
			voteMap: map[string]types.ExchangeRateBallot{},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			result, err := s.keeper.PickReferenceDenom(s.ctx, tc.voteTargets, tc.voteMap)
			s.Require().NoError(err)
			s.Require().Equal(tc.expectedDenom, result)

			for _, denom := range tc.prunedTargets {
				_, ok := tc.voteTargets[denom]
				s.Require().False(ok)
			}
			for _, denom := range tc.prunedVotes {
				_, ok := tc.voteMap[denom]
				s.Require().False(ok)
			}
		})
	}
}

func (s *KeeperTestSuite) TestBuildValidatorClaimMap() {
	tests := []struct {
		name          string
		setup         func()
		expectedPower map[string]int64
	}{
		{
			name: "two bonded validators",
			setup: func() {
				s.setupBuildValidatorClaimMapMocks(
					[]sdk.ValAddress{valAddr1, valAddr2},
					[]int64{10, 20},
					100,
				)
			},
			expectedPower: map[string]int64{
				operStr(valAddr1): 10,
				operStr(valAddr2): 20,
			},
		},
		{
			name: "max validators caps result",
			setup: func() {
				s.stakingKeeper.EXPECT().MaxValidators(s.ctx).Return(uint32(1))
				s.stakingKeeper.EXPECT().ValidatorsPowerStoreIterator(s.ctx).Return(newMockIterator([]byte(valAddr1), []byte(valAddr2)))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(makeValidator(valAddr1, stakingtypes.Bonded, 10))
				s.stakingKeeper.EXPECT().ValidatorAddressCodec().Return(valCodec)
			},
			expectedPower: map[string]int64{
				operStr(valAddr1): 10,
			},
		},
		{
			name: "no validators",
			setup: func() {
				s.stakingKeeper.EXPECT().MaxValidators(s.ctx).Return(uint32(100))
				s.stakingKeeper.EXPECT().ValidatorsPowerStoreIterator(s.ctx).Return(newMockIterator())
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
			},
			expectedPower: map[string]int64{},
		},
		{
			name: "unbonded validator skipped",
			setup: func() {
				s.stakingKeeper.EXPECT().MaxValidators(s.ctx).Return(uint32(100))
				s.stakingKeeper.EXPECT().ValidatorsPowerStoreIterator(s.ctx).Return(newMockIterator([]byte(valAddr1)))
				s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))
				s.stakingKeeper.EXPECT().Validator(s.ctx, valAddr1).Return(makeValidator(valAddr1, stakingtypes.Unbonded, 10))
			},
			expectedPower: map[string]int64{},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			claims, err := s.keeper.BuildValidatorClaimMap(s.ctx)
			s.Require().NoError(err)
			s.Require().Len(claims, len(tc.expectedPower))

			for operator, power := range tc.expectedPower {
				claim, ok := claims[operator]
				s.Require().True(ok)
				s.Require().Equal(power, claim.Power)
				s.Require().Equal(int64(0), claim.Weight)
				s.Require().Equal(int64(0), claim.WinCount)
			}
		})
	}
}

func (s *KeeperTestSuite) TestCountMisses() {
	tests := []struct {
		name           string
		setup          func()
		voteTargets    map[string]math.LegacyDec
		claimMap       map[string]types.Claim
		expectedMisses map[string]uint64
	}{
		{
			name: "increments only validators missing a passing denom",
			setup: func() {
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 2))
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr2, 7))
			},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDec(1),
				core.MicroUSDDenom: math.LegacyNewDec(1),
			},
			claimMap: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 0, 1, valAddr1),
				operStr(valAddr2): types.NewClaim(10, 0, 2, valAddr2),
			},
			expectedMisses: map[string]uint64{
				valAddr1.String(): 3,
				valAddr2.String(): 7,
			},
		},
		{
			name:  "creates counter for first miss",
			setup: func() {},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDec(1),
			},
			claimMap: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 0, 0, valAddr1),
			},
			expectedMisses: map[string]uint64{
				valAddr1.String(): 1,
			},
		},
		{
			name:        "empty vote targets means no misses",
			setup:       func() {},
			voteTargets: map[string]math.LegacyDec{},
			claimMap: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 0, 0, valAddr1),
			},
			expectedMisses: map[string]uint64{},
		},
		{
			name:  "all validators voted all targets",
			setup: func() {},
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: math.LegacyNewDec(1),
				core.MicroUSDDenom: math.LegacyNewDec(1),
			},
			claimMap: map[string]types.Claim{
				operStr(valAddr1): types.NewClaim(10, 0, 2, valAddr1),
				operStr(valAddr2): types.NewClaim(10, 0, 2, valAddr2),
			},
			expectedMisses: map[string]uint64{},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			err := s.keeper.CountMisses(s.ctx, tc.voteTargets, tc.claimMap)
			s.Require().NoError(err)

			for operator, expected := range tc.expectedMisses {
				valAddr, err := sdk.ValAddressFromBech32(operator)
				s.Require().NoError(err)

				missCount, err := s.keeper.MissCounter.Get(s.ctx, valAddr)
				s.Require().NoError(err)
				s.Require().Equal(expected, missCount)
			}

			for _, operator := range []sdk.ValAddress{valAddr1, valAddr2} {
				if _, ok := tc.expectedMisses[operator.String()]; ok {
					continue
				}

				_, err := s.keeper.MissCounter.Get(s.ctx, operator)
				s.Require().True(errors.Is(err, collections.ErrNotFound), "expected no miss counter for %s, got %v", operator.String(), err)
			}
		})
	}
}
