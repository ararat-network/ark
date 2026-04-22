package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestUpdateExchangeRates() {
	tobinTax := math.LegacyNewDecWithPrec(25, 4)
	defaultBondedTokens := math.NewInt(2_000_000)
	valAddr1Str := valAddr1.String()
	valAddr2Str := valAddr2.String()

	tests := []struct {
		name           string
		voteTargets    map[string]math.LegacyDec
		votes          []types.Vote
		bondedTokens   math.Int
		seedStaleRate  bool
		expectedDenoms []string // nil = expect no rates set
	}{
		{
			name: "single denom rate set from ballot median",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
			},
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)}},
					Voter:         valAddr1Str,
				},
				{
					ExchangeRates: types.ExchangeRates{{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)}},
					Voter:         valAddr2Str,
				},
			},
			expectedDenoms: []string{core.MicroKRWDenom},
		},
		{
			name: "multiple denoms cross-rate computed via reference",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
				core.MicroUSDDenom: tobinTax,
			},
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
						{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDec(1)},
					},
					Voter: valAddr1Str,
				},
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
						{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDec(1)},
					},
					Voter: valAddr2Str,
				},
			},
			expectedDenoms: []string{core.MicroKRWDenom, core.MicroUSDDenom},
		},
		{
			name: "ballot below threshold no rates set",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
			},
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)}},
					Voter:         valAddr1Str,
				},
			},
			bondedTokens: math.NewInt(100_000_000),
		},
		{
			name: "no votes no rates set",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
			},
		},
		{
			name: "clears pre-existing exchange rate",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
			},
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)}},
					Voter:         valAddr1Str,
				},
				{
					ExchangeRates: types.ExchangeRates{{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)}},
					Voter:         valAddr2Str,
				},
			},
			seedStaleRate:  true,
			expectedDenoms: []string{core.MicroKRWDenom},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, vote := range tc.votes {
				voter, err := sdk.ValAddressFromBech32(vote.Voter)
				s.Require().NoError(err)
				s.Require().NoError(s.keeper.Vote.Set(s.ctx, voter, vote))
			}
			if tc.seedStaleRate {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, "ustale", math.LegacyNewDec(999)))
			}

			bondedTokens := tc.bondedTokens
			if bondedTokens.IsNil() {
				bondedTokens = defaultBondedTokens
			}
			s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(bondedTokens)
			s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))

			validatorClaimMap := map[string]types.ValidatorScore{
				valAddr1Str: types.NewValidatorScore(10, 0, 0, valAddr1),
				valAddr2Str: types.NewValidatorScore(10, 0, 0, valAddr2),
			}

			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)

			err = s.keeper.UpdateExchangeRates(
				s.ctx,
				params.RewardBand,
				params.VoteThreshold,
				tc.voteTargets,
				validatorClaimMap,
			)
			s.Require().NoError(err)

			for _, denom := range tc.expectedDenoms {
				rate, err := s.keeper.ExchangeRate.Get(s.ctx, denom)
				s.Require().NoError(err, "expected rate for %s", denom)
				s.Require().True(rate.IsPositive(), "rate for %s should be positive, got %s", denom, rate)
			}

			// Verify no extra rates (catches stale rates that should have been cleared)
			count := 0
			_ = s.keeper.ExchangeRate.Walk(s.ctx, nil, func(_ string, _ math.LegacyDec) (bool, error) {
				count++
				return false, nil
			})
			s.Require().Equal(len(tc.expectedDenoms), count, "unexpected exchange rate count")
		})
	}
}

func (s *KeeperTestSuite) TestGroupVotesByDenom() {
	valAddr1Str := valAddr1.String()
	valAddr2Str := valAddr2.String()

	defaultScoreMap := map[string]types.ValidatorScore{
		valAddr1Str: types.NewValidatorScore(10, 0, 0, valAddr1),
		valAddr2Str: types.NewValidatorScore(20, 0, 0, valAddr2),
	}

	tests := []struct {
		name     string
		votes    []types.Vote
		scoreMap map[string]types.ValidatorScore
		expected map[string]types.DenomVotes
	}{
		{
			name:     "no votes",
			scoreMap: defaultScoreMap,
			expected: map[string]types.DenomVotes{},
		},
		{
			name: "single validator, single denom",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
					},
					Voter: valAddr1Str,
				},
			},
			scoreMap: defaultScoreMap,
			expected: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
				},
			},
		},
		{
			name: "single validator, multiple denoms",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
						{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDec(1)},
					},
					Voter: valAddr1Str,
				},
			},
			scoreMap: defaultScoreMap,
			expected: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
				},
				core.MicroUSDDenom: {
					types.NewDenomVote(math.LegacyNewDec(1), core.MicroUSDDenom, valAddr1, 10),
				},
			},
		},
		{
			name: "multiple validators sorted by rate",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(2000)},
					},
					Voter: valAddr1Str,
				},
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
					},
					Voter: valAddr2Str,
				},
			},
			scoreMap: defaultScoreMap,
			expected: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr2, 20),
					types.NewDenomVote(math.LegacyNewDec(2000), core.MicroKRWDenom, valAddr1, 10),
				},
			},
		},
		{
			name: "validator not in score map is filtered out",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
					},
					Voter: valAddr1Str,
				},
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
					},
					Voter: valAddr2Str,
				},
			},
			scoreMap: map[string]types.ValidatorScore{
				valAddr1Str: types.NewValidatorScore(10, 0, 0, valAddr1),
			},
			expected: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
				},
			},
		},
		{
			name: "all validators filtered",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
					},
					Voter: valAddr1Str,
				},
			},
			scoreMap: map[string]types.ValidatorScore{},
			expected: map[string]types.DenomVotes{},
		},
		{
			name: "zero rate vote has zero power",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyZeroDec()},
					},
					Voter: valAddr1Str,
				},
			},
			scoreMap: defaultScoreMap,
			expected: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyZeroDec(), core.MicroKRWDenom, valAddr1, 0),
				},
			},
		},
		{
			name: "negative rate vote has zero power",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(-1)},
					},
					Voter: valAddr1Str,
				},
			},
			scoreMap: defaultScoreMap,
			expected: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(-1), core.MicroKRWDenom, valAddr1, 0),
				},
			},
		},
		{
			name: "positive vote uses score power",
			votes: []types.Vote{
				{
					ExchangeRates: types.ExchangeRates{
						{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)},
					},
					Voter: valAddr1Str,
				},
			},
			scoreMap: map[string]types.ValidatorScore{
				valAddr1Str: types.NewValidatorScore(42, 0, 0, valAddr1),
			},
			expected: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 42),
				},
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, vote := range tc.votes {
				voter, err := sdk.ValAddressFromBech32(vote.Voter)
				s.Require().NoError(err)
				s.Require().NoError(s.keeper.Vote.Set(s.ctx, voter, vote))
			}

			result, err := s.keeper.GroupVotesByDenom(s.ctx, tc.scoreMap)
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, result)
		})
	}
}

func (s *KeeperTestSuite) TestPickReferenceDenom() {
	tobinTax := math.LegacyNewDecWithPrec(25, 4)
	voteThreshold := math.LegacyNewDecWithPrec(50, 2)

	tests := []struct {
		name          string
		voteTargets   map[string]math.LegacyDec
		voteMap       map[string]types.DenomVotes
		expectedDenom string
		prunedTargets []string
		prunedVotes   []string
	}{
		{
			name: "single denom above threshold",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
			},
			voteMap: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
					types.NewDenomVote(math.LegacyNewDec(1001), core.MicroKRWDenom, valAddr2, 10),
				},
			},
			expectedDenom: core.MicroKRWDenom,
		},
		{
			name: "higher turnout wins",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
				core.MicroUSDDenom: tobinTax,
			},
			voteMap: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
					types.NewDenomVote(math.LegacyNewDec(1001), core.MicroKRWDenom, valAddr2, 10),
				},
				core.MicroUSDDenom: {
					types.NewDenomVote(math.LegacyNewDec(1), core.MicroUSDDenom, valAddr1, 10),
				},
			},
			expectedDenom: core.MicroKRWDenom,
		},
		{
			name: "same turnout chooses alphabetical denom",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
				core.MicroUSDDenom: tobinTax,
			},
			voteMap: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 10),
					types.NewDenomVote(math.LegacyNewDec(1001), core.MicroKRWDenom, valAddr2, 10),
				},
				core.MicroUSDDenom: {
					types.NewDenomVote(math.LegacyNewDec(1), core.MicroUSDDenom, valAddr1, 10),
					types.NewDenomVote(math.LegacyNewDec(2), core.MicroUSDDenom, valAddr2, 10),
				},
			},
			expectedDenom: core.MicroKRWDenom,
		},
		{
			name: "below threshold gets pruned",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
			},
			voteMap: map[string]types.DenomVotes{
				core.MicroKRWDenom: {
					types.NewDenomVote(math.LegacyNewDec(1000), core.MicroKRWDenom, valAddr1, 1),
				},
			},
			prunedTargets: []string{core.MicroKRWDenom},
			prunedVotes:   []string{core.MicroKRWDenom},
		},
		{
			name:        "denom missing from vote targets pruned from vote map",
			voteTargets: map[string]math.LegacyDec{},
			voteMap: map[string]types.DenomVotes{
				"ufoo": {
					types.NewDenomVote(math.LegacyNewDec(1), "ufoo", valAddr1, 10),
				},
			},
			prunedVotes: []string{"ufoo"},
		},
		{
			name: "no votes returns empty denom",
			voteTargets: map[string]math.LegacyDec{
				core.MicroKRWDenom: tobinTax,
			},
			voteMap: map[string]types.DenomVotes{},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.stakingKeeper.EXPECT().TotalBondedTokens(s.ctx).Return(math.NewInt(20_000_000))
			s.stakingKeeper.EXPECT().PowerReduction(s.ctx).Return(math.NewInt(1_000_000))

			result, err := s.keeper.PickReferenceDenom(s.ctx, voteThreshold, tc.voteTargets, tc.voteMap)
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

func (s *KeeperTestSuite) TestTallyVotes() {
	valAddr3 := sdk.ValAddress([]byte("validator3___________"))
	valAddr1Str := valAddr1.String()
	valAddr2Str := valAddr2.String()
	valAddr3Str := valAddr3.String()

	tests := []struct {
		name           string
		rewardBand     math.LegacyDec
		votes          types.DenomVotes
		scoreMap       map[string]types.ValidatorScore
		expectedMedian math.LegacyDec
		expectedScores map[string]types.ValidatorScore
	}{
		{
			name:       "rewards votes inside spread",
			rewardBand: math.LegacyNewDecWithPrec(2, 2),
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyNewDec(100), core.MicroKRWDenom, valAddr1, 10),
				types.NewDenomVote(math.LegacyNewDec(101), core.MicroKRWDenom, valAddr2, 20),
				types.NewDenomVote(math.LegacyNewDec(130), core.MicroKRWDenom, valAddr3, 30),
			},
			scoreMap: map[string]types.ValidatorScore{
				valAddr1Str: types.NewValidatorScore(10, 0, 0, valAddr1),
				valAddr2Str: types.NewValidatorScore(20, 0, 0, valAddr2),
				valAddr3Str: types.NewValidatorScore(30, 0, 0, valAddr3),
			},
			expectedMedian: math.LegacyNewDec(101),
			expectedScores: map[string]types.ValidatorScore{
				valAddr1Str: types.NewValidatorScore(10, 10, 1, valAddr1),
				valAddr2Str: types.NewValidatorScore(20, 20, 1, valAddr2),
				valAddr3Str: types.NewValidatorScore(30, 0, 0, valAddr3),
			},
		},
		{
			name:       "abstain vote wins without weight",
			rewardBand: math.LegacyNewDecWithPrec(2, 2),
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyZeroDec(), core.MicroKRWDenom, valAddr1, 0),
				types.NewDenomVote(math.LegacyNewDec(100), core.MicroKRWDenom, valAddr2, 20),
			},
			scoreMap: map[string]types.ValidatorScore{
				valAddr1Str: types.NewValidatorScore(10, 0, 0, valAddr1),
				valAddr2Str: types.NewValidatorScore(20, 0, 0, valAddr2),
			},
			expectedMedian: math.LegacyNewDec(100),
			expectedScores: map[string]types.ValidatorScore{
				valAddr1Str: types.NewValidatorScore(10, 0, 1, valAddr1),
				valAddr2Str: types.NewValidatorScore(20, 20, 1, valAddr2),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			median := s.keeper.ScoreVotes(s.ctx, tc.rewardBand, tc.votes, tc.scoreMap)

			s.Require().True(tc.expectedMedian.Equal(median), "expected %s, got %s", tc.expectedMedian, median)
			s.Require().Equal(tc.expectedScores, tc.scoreMap)
		})
	}
}

func (s *KeeperTestSuite) TestClearVotes() {
	valAddr1Str := valAddr1.String()
	valAddr2Str := valAddr2.String()

	tests := []struct {
		name          string
		blockHeight   int64
		votePeriod    uint64
		votes         []types.Vote
		prevotes      []types.Prevote
		keptPrevoters []sdk.ValAddress
	}{
		{
			name:        "no votes or prevotes",
			blockHeight: 10,
			votePeriod:  5,
		},
		{
			name:        "all votes cleared unconditionally",
			blockHeight: 10,
			votePeriod:  5,
			votes: []types.Vote{
				{Voter: valAddr1Str},
				{Voter: valAddr2Str},
			},
		},
		{
			name:        "expired prevote cleared",
			blockHeight: 10,
			votePeriod:  5,
			prevotes: []types.Prevote{
				{Voter: valAddr1Str, SubmitBlock: 2},
			},
		},
		{
			name:        "prevote just past expiry cleared",
			blockHeight: 10,
			votePeriod:  5,
			prevotes: []types.Prevote{
				{Voter: valAddr1Str, SubmitBlock: 4},
			},
		},
		{
			name:        "prevote at exact expiry boundary kept",
			blockHeight: 10,
			votePeriod:  5,
			prevotes: []types.Prevote{
				{Voter: valAddr1Str, SubmitBlock: 5},
			},
			keptPrevoters: []sdk.ValAddress{valAddr1},
		},
		{
			name:        "fresh prevote kept",
			blockHeight: 10,
			votePeriod:  5,
			prevotes: []types.Prevote{
				{Voter: valAddr1Str, SubmitBlock: 8},
			},
			keptPrevoters: []sdk.ValAddress{valAddr1},
		},
		{
			name:        "expired prevote cleared, fresh kept, all votes cleared",
			blockHeight: 10,
			votePeriod:  5,
			votes: []types.Vote{
				{Voter: valAddr1Str},
			},
			prevotes: []types.Prevote{
				{Voter: valAddr1Str, SubmitBlock: 1},
				{Voter: valAddr2Str, SubmitBlock: 8},
			},
			keptPrevoters: []sdk.ValAddress{valAddr2},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			sdkCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(tc.blockHeight)
			s.ctx = sdkCtx

			for _, vote := range tc.votes {
				voter, err := sdk.ValAddressFromBech32(vote.Voter)
				s.Require().NoError(err)
				s.Require().NoError(s.keeper.Vote.Set(s.ctx, voter, vote))
			}
			for _, prevote := range tc.prevotes {
				voter, err := sdk.ValAddressFromBech32(prevote.Voter)
				s.Require().NoError(err)
				s.Require().NoError(s.keeper.Prevote.Set(s.ctx, voter, prevote))
			}

			err := s.keeper.ClearVotes(s.ctx, tc.votePeriod)
			s.Require().NoError(err)

			s.Require().Zero(s.countVotes(), "unexpected vote count")
			s.Require().Equal(len(tc.keptPrevoters), s.countPrevotes(), "unexpected prevote count")

			for _, voter := range tc.keptPrevoters {
				_, err := s.keeper.Prevote.Get(s.ctx, voter)
				s.Require().NoError(err)
			}
		})
	}
}

func (s *KeeperTestSuite) countVotes() int {
	count := 0
	err := s.keeper.Vote.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.Vote) (bool, error) {
		count++
		return false, nil
	})
	s.Require().NoError(err)
	return count
}

func (s *KeeperTestSuite) countPrevotes() int {
	count := 0
	err := s.keeper.Prevote.Walk(s.ctx, nil, func(_ sdk.ValAddress, _ types.Prevote) (bool, error) {
		count++
		return false, nil
	})
	s.Require().NoError(err)
	return count
}
