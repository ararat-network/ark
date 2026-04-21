package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

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
