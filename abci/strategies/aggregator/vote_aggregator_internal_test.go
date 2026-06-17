package aggregator

import (
	"testing"

	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	abcitestutil "noah/abci/testutil"
	oracletypes "noah/x/oracle/types"
)

func TestAggregateOracleVotesKeepsNoVoteTargetAccountable(t *testing.T) {
	votes := []Vote{
		newTestVote(t, []byte{1}, 1, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
		newTestVote(t, []byte{2}, 10, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
		"ukrw": math.LegacyZeroDec(),
	}

	prices, scoreMap, err := NewVoteAggregator(log.NewNopLogger()).
		AggregateOracleVotes(sdk.Context{}, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "uusd")
	require.NotContains(t, prices, "ukrw")
	require.Contains(t, voteTargets, "uusd")
	require.Contains(t, voteTargets, "ukrw")
	require.Len(t, scoreMap, 2)

	for _, vote := range votes {
		score := scoreMap[sdk.ConsAddress(vote.Validator.Address).String()]
		require.Equal(t, uint64(1), score.WinCount)
	}
}

func TestAggregateOracleVotesKeepsFailedQuorumTargetAccountable(t *testing.T) {
	testCases := []struct {
		name string
		rate math.LegacyDec
	}{
		{
			name: "positive rate",
			rate: math.LegacyNewDec(1000),
		},
		{
			name: "zero rate",
			rate: math.LegacyZeroDec(),
		},
		{
			name: "negative rate",
			rate: math.LegacyNewDec(-1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			voterWithFailedQuorumDenom := []byte{1}
			voterMissingFailedQuorumDenom := []byte{2}
			votes := []Vote{
				newTestVote(t, voterWithFailedQuorumDenom, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
					"ukrw": tc.rate,
				}),
				newTestVote(t, voterMissingFailedQuorumDenom, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(75, 2)
			voteTargets := map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
				"ukrw": math.LegacyZeroDec(),
			}

			prices, scoreMap, err := NewVoteAggregator(log.NewNopLogger()).
				AggregateOracleVotes(sdk.Context{}, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "uusd")
			require.NotContains(t, prices, "ukrw")
			require.Contains(t, voteTargets, "uusd")
			require.Contains(t, voteTargets, "ukrw")

			score := scoreMap[sdk.ConsAddress(voterWithFailedQuorumDenom).String()]
			require.Equal(t, uint64(2), score.WinCount)

			score = scoreMap[sdk.ConsAddress(voterMissingFailedQuorumDenom).String()]
			require.Equal(t, uint64(1), score.WinCount)
		})
	}
}

func TestAggregateOracleVotesCountsNonPositiveTargetRatesAsSubmitted(t *testing.T) {
	testCases := []struct {
		name string
		rate math.LegacyDec
	}{
		{
			name: "zero rate",
			rate: math.LegacyZeroDec(),
		},
		{
			name: "negative rate",
			rate: math.LegacyNewDec(-1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			positiveVoter := []byte{1}
			nonPositiveVoter := []byte{2}
			votes := []Vote{
				newTestVote(t, positiveVoter, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
				newTestVote(t, nonPositiveVoter, 10, map[string]math.LegacyDec{
					"uusd": tc.rate,
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
			voteTargets := map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
			}

			prices, scoreMap, err := NewVoteAggregator(log.NewNopLogger()).
				AggregateOracleVotes(sdk.Context{}, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "uusd")

			score := scoreMap[sdk.ConsAddress(positiveVoter).String()]
			require.Equal(t, uint64(1), score.WinCount)
			require.Equal(t, uint64(10), score.Weight)

			score = scoreMap[sdk.ConsAddress(nonPositiveVoter).String()]
			require.Equal(t, uint64(1), score.WinCount)
			require.Zero(t, score.Weight)
		})
	}
}

func TestAggregateOracleVotesPenalizesPositiveOutOfBandTargetRates(t *testing.T) {
	testCases := []struct {
		name      string
		outOfBand math.LegacyDec
	}{
		{
			name:      "positive out of band rate",
			outOfBand: math.LegacyNewDec(1000),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inBandVoter1 := []byte{1}
			inBandVoter2 := []byte{2}
			outOfBandVoter := []byte{3}
			votes := []Vote{
				newTestVote(t, inBandVoter1, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
				newTestVote(t, inBandVoter2, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
				newTestVote(t, outOfBandVoter, 10, map[string]math.LegacyDec{
					"uusd": tc.outOfBand,
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
			voteTargets := map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
			}

			prices, scoreMap, err := NewVoteAggregator(log.NewNopLogger()).
				AggregateOracleVotes(sdk.Context{}, votes, params, voteTargets)

			require.NoError(t, err)
			require.True(t, math.LegacyNewDec(100).Equal(prices["uusd"]))

			score := scoreMap[sdk.ConsAddress(inBandVoter1).String()]
			require.Equal(t, uint64(1), score.WinCount)
			require.Equal(t, uint64(10), score.Weight)

			score = scoreMap[sdk.ConsAddress(inBandVoter2).String()]
			require.Equal(t, uint64(1), score.WinCount)
			require.Equal(t, uint64(10), score.Weight)

			score = scoreMap[sdk.ConsAddress(outOfBandVoter).String()]
			require.Zero(t, score.WinCount)
			require.Zero(t, score.Weight)
		})
	}
}

func newTestVote(t *testing.T, address []byte, power int64, rates map[string]math.LegacyDec) Vote {
	t.Helper()

	return Vote{
		Validator: cometabci.Validator{
			Address: address,
			Power:   power,
		},
		OracleVoteExtension: abcitestutil.NewOracleVoteExtension(t, rates),
	}
}
