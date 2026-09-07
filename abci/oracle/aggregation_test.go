package oracle_test

import (
	"context"
	"math/big"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	"github.com/ararat-network/ark/abci/oracle"
	abcitestutil "github.com/ararat-network/ark/abci/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestAggregateOracleVotesLeavesOmittedTargetUnpriced(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{2}, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := []string{"ausd", "akrw"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "ausd")
	require.NotContains(t, prices, "akrw")
	require.Len(t, keeper.scoreWeights, 2)

	for _, vote := range votes {
		require.True(t, math.NewInt(vote.power).Equal(keeper.scoreWeights[vote.validator.String()]))
		require.Equal(t, uint64(1), keeper.eligibleCounts[vote.validator.String()])
		require.Equal(t, uint64(1), keeper.attendedCounts[vote.validator.String()])
	}
}

func TestAggregateOracleVotesDoesNotPunishFailedQuorumVotes(t *testing.T) {
	testCases := []struct {
		name string
		rate math.LegacyDec
	}{
		{
			name: "positive rate",
			rate: math.LegacyNewDec(1000),
		},
		{
			name: "zero rate abstention",
			rate: math.LegacyZeroDec(),
		},
		{
			name: "negative rate abstention",
			rate: math.LegacyNewDec(-1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			voterWithFailedQuorumDenom := []byte{1}
			voterMissingFailedQuorumDenom := []byte{2}
			votes := []testVote{
				newTestVote(voterWithFailedQuorumDenom, 10, map[string]math.LegacyDec{
					"ausd": math.LegacyNewDec(100),
					"akrw": tc.rate,
				}),
				newTestVote(voterMissingFailedQuorumDenom, 10, map[string]math.LegacyDec{
					"ausd": math.LegacyNewDec(100),
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(75, 2)
			voteTargets := []string{"ausd", "akrw"}

			keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "ausd")
			require.NotContains(t, prices, "akrw")
			// Both voters price ausd, so the failed akrw quorum costs neither of
			// them attendance.
			require.Equal(t, uint64(1), keeper.attendedCounts[consKey(voterWithFailedQuorumDenom)])
			require.Equal(t, uint64(1), keeper.attendedCounts[consKey(voterMissingFailedQuorumDenom)])
		})
	}
}

func TestAggregateOracleVotesSkipsCrossRateDenomWithoutReferenceOverlap(t *testing.T) {
	referenceOnlyVoter := []byte{1}
	crossOnlyVoter := []byte{2}
	votes := []testVote{
		newTestVote(referenceOnlyVoter, 10, map[string]math.LegacyDec{
			"akrw": math.LegacyNewDec(1000),
		}),
		newTestVote(crossOnlyVoter, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := []string{"akrw", "ausd"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "akrw")
	require.NotContains(t, prices, "ausd")
	require.Len(t, keeper.scoreWeights, 2)
}

func TestAggregateOracleVotesChoosesReferenceWithBestOverlapCoverage(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 40, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{2}, 20, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
			"akrw": math.LegacyNewDec(1000),
			"axdr": math.LegacyNewDec(2),
		}),
		newTestVote([]byte{3}, 30, map[string]math.LegacyDec{
			"akrw": math.LegacyNewDec(900),
			"axdr": math.LegacyNewDec(3),
		}),
		newTestVote([]byte{4}, 10, map[string]math.LegacyDec{}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := []string{"ausd", "akrw", "axdr"}

	_, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.NotContains(t, prices, "ausd")
	require.True(t, math.LegacyNewDec(900).Equal(prices["akrw"]))
	require.True(t, math.LegacyNewDec(3).Equal(prices["axdr"]))
}

func TestAggregateOracleVotesSkipsCrossRateDenomBelowOverlapQuorum(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 40, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
			"akrw": math.LegacyNewDec(1000),
		}),
		newTestVote([]byte{2}, 40, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{3}, 20, map[string]math.LegacyDec{
			"akrw": math.LegacyNewDec(1000),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := []string{"ausd", "akrw"}

	_, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "ausd")
	require.NotContains(t, prices, "akrw")
}

func TestAggregateOracleVotesRecordsSoleTargetAbstentionAsEligibleOnly(t *testing.T) {
	testCases := []struct {
		name string
		rate math.LegacyDec
	}{
		{
			name: "zero rate on the only target",
			rate: math.LegacyZeroDec(),
		},
		{
			name: "negative rate on the only target",
			rate: math.LegacyNewDec(-1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			positiveVoter := []byte{1}
			abstainingVoter := []byte{2}
			votes := []testVote{
				newTestVote(positiveVoter, 10, map[string]math.LegacyDec{
					"ausd": math.LegacyNewDec(100),
				}),
				newTestVote(abstainingVoter, 10, map[string]math.LegacyDec{
					"ausd": tc.rate,
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
			voteTargets := []string{"ausd"}

			keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "ausd")
			require.True(t, math.NewInt(10).Equal(keeper.scoreWeights[consKey(positiveVoter)]))
			require.True(t, keeper.scoreWeights[consKey(abstainingVoter)].IsZero())
			require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(abstainingVoter)])
			require.Zero(t, keeper.attendedCounts[consKey(abstainingVoter)])
			require.Equal(t, uint64(1), keeper.attendedCounts[consKey(positiveVoter)])
		})
	}
}

func TestAggregateOracleVotesRecordsEveryTargetAbstentionAsEligibleOnly(t *testing.T) {
	honestVoter := []byte{1}
	abstainingVoter := []byte{2}
	votes := []testVote{
		newTestVote(honestVoter, 10, map[string]math.LegacyDec{
			"akrw": math.LegacyNewDec(1000),
			"axdr": math.LegacyNewDec(2),
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(abstainingVoter, 10, map[string]math.LegacyDec{
			"akrw": math.LegacyZeroDec(),
			"axdr": math.LegacyNewDec(-1),
			"ausd": math.LegacyZeroDec(),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := []string{"akrw", "axdr", "ausd"}

	keeper, _, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.True(t, math.NewInt(30).Equal(keeper.scoreWeights[consKey(honestVoter)]))
	require.True(t, keeper.scoreWeights[consKey(abstainingVoter)].IsZero())
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(abstainingVoter)])
	require.Zero(t, keeper.attendedCounts[consKey(abstainingVoter)])
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(honestVoter)])
}

func TestAggregateOracleVotesCountsPartialAbstentionAsAttended(t *testing.T) {
	honestVoter := []byte{1}
	abstainingVoter := []byte{2}
	votes := []testVote{
		newTestVote(honestVoter, 10, map[string]math.LegacyDec{
			"akrw": math.LegacyNewDec(1000),
			"axdr": math.LegacyNewDec(2),
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(abstainingVoter, 10, map[string]math.LegacyDec{
			"akrw": math.LegacyZeroDec(),
			"axdr": math.LegacyNewDec(-1),
			"ausd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := []string{"akrw", "axdr", "ausd"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "ausd")
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(abstainingVoter)])
	require.True(t, math.NewInt(10).Equal(keeper.scoreWeights[consKey(abstainingVoter)]))
	require.True(t, math.NewInt(30).Equal(keeper.scoreWeights[consKey(honestVoter)]))
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(honestVoter)])
}

func TestAggregateOracleVotesExcludesAbstentionFromPriceQuorum(t *testing.T) {
	abstainingVoter := []byte{1}
	positiveVoter := []byte{2}
	votes := []testVote{
		newTestVote(abstainingVoter, 40, map[string]math.LegacyDec{
			"akrw": math.LegacyNewDec(1000),
			"ausd": math.LegacyZeroDec(),
		}),
		newTestVote(positiveVoter, 60, map[string]math.LegacyDec{
			"akrw": math.LegacyNewDec(1000),
			"ausd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(67, 2)
	voteTargets := []string{"akrw", "ausd"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	// The abstaining 40 power adds no ballot power, so ausd's 60 positive power
	// stays below the 67 quorum and publishes no price.
	require.NotContains(t, prices, "ausd")
	require.Contains(t, prices, "akrw")
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(abstainingVoter)])
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(positiveVoter)])
	require.True(t, math.NewInt(40).Equal(keeper.scoreWeights[consKey(abstainingVoter)]))
	require.True(t, math.NewInt(60).Equal(keeper.scoreWeights[consKey(positiveVoter)]))
}

func TestAggregateOracleVotesGivesOutOfBandVotesNoRewardWeight(t *testing.T) {
	inBandVoter1 := []byte{1}
	inBandVoter2 := []byte{2}
	outOfBandVoter := []byte{3}
	votes := []testVote{
		newTestVote(inBandVoter1, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(inBandVoter2, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(outOfBandVoter, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(1000),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := []string{"ausd"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(100).Equal(prices["ausd"]))
	require.True(t, math.NewInt(10).Equal(keeper.scoreWeights[consKey(inBandVoter1)]))
	require.True(t, math.NewInt(10).Equal(keeper.scoreWeights[consKey(inBandVoter2)]))
	require.True(t, keeper.scoreWeights[consKey(outOfBandVoter)].IsZero())
	// An out-of-band price still participates: it earns nothing but is not an
	// attendance failure.
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(outOfBandVoter)])
}

func TestAggregateOracleVotesUsesCeilingForVoteThreshold(t *testing.T) {
	testCases := []struct {
		name         string
		reportPower  int64
		absentPower  int64
		expectsPrice bool
	}{
		{
			name:         "rounded support below threshold fails",
			reportPower:  5,
			absentPower:  5,
			expectsPrice: false,
		},
		{
			name:         "support at ceiling threshold passes",
			reportPower:  6,
			absentPower:  4,
			expectsPrice: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			votes := []testVote{
				newTestVote([]byte{1}, tc.reportPower, map[string]math.LegacyDec{
					"ausd": math.LegacyNewDec(100),
				}),
				newTestVote([]byte{2}, tc.absentPower, map[string]math.LegacyDec{}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(51, 2)
			voteTargets := []string{"ausd"}

			_, prices, err := processVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			if tc.expectsPrice {
				require.Contains(t, prices, "ausd")
			} else {
				require.NotContains(t, prices, "ausd")
			}
		})
	}
}

func TestAggregateOracleVotesUsesCeilingForOverlapThreshold(t *testing.T) {
	testCases := []struct {
		name          string
		overlapPower  int64
		referenceOnly int64
		targetOnly    int64
		absentPower   int64
		expectsTarget bool
	}{
		{
			name:          "rounded overlap below threshold fails",
			overlapPower:  5,
			referenceOnly: 1,
			targetOnly:    1,
			absentPower:   3,
			expectsTarget: false,
		},
		{
			name:          "overlap at ceiling threshold passes",
			overlapPower:  6,
			referenceOnly: 0,
			targetOnly:    0,
			absentPower:   4,
			expectsTarget: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			votes := []testVote{
				newTestVote([]byte{1}, tc.overlapPower, map[string]math.LegacyDec{
					"aaaa": math.LegacyNewDec(100),
					"azzz": math.LegacyNewDec(10),
				}),
				newTestVote([]byte{2}, tc.referenceOnly, map[string]math.LegacyDec{
					"aaaa": math.LegacyNewDec(100),
				}),
				newTestVote([]byte{3}, tc.targetOnly, map[string]math.LegacyDec{
					"azzz": math.LegacyNewDec(10),
				}),
				newTestVote([]byte{4}, tc.absentPower, map[string]math.LegacyDec{}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(51, 2)
			voteTargets := []string{"aaaa", "azzz"}

			_, prices, err := processVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "aaaa")
			if tc.expectsTarget {
				require.Contains(t, prices, "azzz")
			} else {
				require.NotContains(t, prices, "azzz")
			}
		})
	}
}

func TestAggregateOracleVotesUsesMedianOfValidatorCrossRates(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(1),
			"azzz": math.LegacyNewDec(1),
		}),
		newTestVote([]byte{2}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(100),
			"azzz": math.LegacyNewDec(10),
		}),
		newTestVote([]byte{3}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(101),
			"azzz": math.LegacyNewDec(101),
		}),
	}
	voteTargets := []string{"aaaa", "azzz"}

	keeper, prices, err := processVoteExtensions(t, votes, oracletypes.DefaultParams(), voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(100).Equal(prices["aaaa"]))
	// Per-validator aaaa/azzz ratios are 1, 10, and 1, so their median is 1.
	// This deliberately differs from median(aaaa)/median(azzz) = 100/10.
	require.True(t, math.LegacyNewDec(100).Equal(prices["azzz"]))
	require.True(t, math.NewInt(1).Equal(keeper.scoreWeights[consKey([]byte{1})]))
	require.True(t, math.NewInt(1).Equal(keeper.scoreWeights[consKey([]byte{2})]))
	require.True(t, math.NewInt(2).Equal(keeper.scoreWeights[consKey([]byte{3})]))
}

func TestAggregateOracleVotesSkipsUnrepresentableCrossRateObservation(t *testing.T) {
	honestVoter := []byte{1}
	extremeVoter := []byte{2}
	votes := []testVote{
		newTestVote(honestVoter, 90, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(100),
			"azzz": math.LegacyNewDec(10),
		}),
		newTestVote(extremeVoter, 10, map[string]math.LegacyDec{
			// Vote rates are bounded to MaxEncodedVoteRateBytes, so no
			// transported pair can overflow a cross-rate quotient; the
			// reachable unrepresentable case is a quotient that rounds to
			// zero: the smallest reference report over the largest
			// permitted target report. The overflow arm stays covered by
			// the ballot-level cross-rate tests.
			"aaaa": math.LegacySmallestDec(),
			"azzz": math.LegacyNewDecFromBigIntWithPrec(
				new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 8*oracletypes.MaxEncodedVoteRateBytes), big.NewInt(1)),
				math.LegacyPrecision,
			),
		}),
	}
	voteTargets := []string{"aaaa", "azzz"}

	keeper, prices, err := processVoteExtensions(t, votes, oracletypes.DefaultParams(), voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(100).Equal(prices["aaaa"]))
	require.True(t, math.LegacyNewDec(10).Equal(prices["azzz"]))
	require.True(t, math.NewInt(180).Equal(keeper.scoreWeights[consKey(honestVoter)]))
	require.True(t, keeper.scoreWeights[consKey(extremeVoter)].IsZero())
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(extremeVoter)])
}

// TestAggregateOracleVotesOmitsDerivedPriceAboveStoreBound pins the one way
// a price can leave the bound every report is held to: the cross rate rounds
// at eighteen decimals, and dividing the reference median by a cross that
// rounded down lands above the largest report that produced it. Here a target
// reported exactly at MaxExchangeRate against a reference of 1500 gives a
// cross of ~4.4e-18, rounded to 4e-18, and a derived price of 3.75e20 —
// above the bound, so the target is omitted rather than stored.
func TestAggregateOracleVotesOmitsDerivedPriceAboveStoreBound(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(1500),
			"azzz": oracletypes.MaxExchangeRate,
		}),
		newTestVote([]byte{2}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(1500),
			"azzz": oracletypes.MaxExchangeRate,
		}),
	}
	voteTargets := []string{"aaaa", "azzz"}

	keeper, prices, err := processVoteExtensions(t, votes, oracletypes.DefaultParams(), voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(1500).Equal(prices["aaaa"]))
	_, priced := prices["azzz"]
	require.False(t, priced)
	// Only the reference target scores.
	require.True(t, math.NewInt(1).Equal(keeper.scoreWeights[consKey([]byte{1})]))
	require.True(t, math.NewInt(1).Equal(keeper.scoreWeights[consKey([]byte{2})]))
}

// TestAggregateOracleVotesIsOrientationInvariant documents executably that
// the tally lands in the store's own orientation whichever way reports are
// quoted: the cross-rate derivation is a ratio, so feeding every report's
// reciprocal publishes every price's reciprocal.
func TestAggregateOracleVotesIsOrientationInvariant(t *testing.T) {
	forward := []testVote{
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(2),
			"azzz": math.LegacyNewDec(4),
		}),
		newTestVote([]byte{2}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(8),
			"azzz": math.LegacyNewDec(2),
		}),
		newTestVote([]byte{3}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(4),
			"azzz": math.LegacyNewDec(4),
		}),
	}
	reciprocal := []testVote{
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDecWithPrec(5, 1),
			"azzz": math.LegacyNewDecWithPrec(25, 2),
		}),
		newTestVote([]byte{2}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDecWithPrec(125, 3),
			"azzz": math.LegacyNewDecWithPrec(5, 1),
		}),
		newTestVote([]byte{3}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDecWithPrec(25, 2),
			"azzz": math.LegacyNewDecWithPrec(25, 2),
		}),
	}
	voteTargets := []string{"aaaa", "azzz"}

	_, forwardPrices, err := processVoteExtensions(t, forward, oracletypes.DefaultParams(), voteTargets)
	require.NoError(t, err)
	_, reciprocalPrices, err := processVoteExtensions(t, reciprocal, oracletypes.DefaultParams(), voteTargets)
	require.NoError(t, err)

	// Median aaaa is 4; per-validator crosses are 0.5, 4, and 1, so azzz is
	// 4 / 1 = 4. Every value here is a power of two, so the reciprocals are
	// exact and the comparison is equality rather than tolerance.
	for _, denom := range voteTargets {
		require.True(t, math.LegacyNewDec(4).Equal(forwardPrices[denom]), denom)
		require.True(t, math.LegacyOneDec().Quo(forwardPrices[denom]).Equal(reciprocalPrices[denom]), denom)
	}
}

func TestProcessVoteExtensionsUsesDeterministicWriteOrder(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{3}, 1, map[string]math.LegacyDec{
			"azzz": math.LegacyNewDec(10),
			"aaaa": math.LegacyNewDec(100),
			"ammm": math.LegacyNewDec(50),
		}),
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"ammm": math.LegacyNewDec(50),
			"azzz": math.LegacyNewDec(10),
			"aaaa": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{2}, 1, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(100),
			"ammm": math.LegacyNewDec(50),
			"azzz": math.LegacyNewDec(10),
		}),
	}
	voteTargets := []string{"azzz", "aaaa", "ammm"}

	keeper, _, err := processVoteExtensions(t, votes, oracletypes.DefaultParams(), voteTargets)

	require.NoError(t, err)
	require.Equal(t, []string{"aaaa", "ammm", "azzz"}, keeper.exchangeRateOrder)
	require.Equal(t, []string{
		consKey([]byte{3}),
		consKey([]byte{1}),
		consKey([]byte{2}),
	}, keeper.scoreOrder)
}

func TestAggregateOracleVotesFixedBandSkipsExtremeNonPositiveRate(t *testing.T) {
	inBandVoter1 := []byte{1}
	inBandVoter2 := []byte{2}
	outOfBandVoter := []byte{3}
	nonPositiveVoter := []byte{4}
	votes := []testVote{
		newTestVote(inBandVoter1, 40, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(inBandVoter2, 40, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(outOfBandVoter, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(120),
		}),
		newTestVote(nonPositiveVoter, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(-1_000_000),
		}),
	}
	params := oracletypes.DefaultParams()
	voteTargets := []string{"ausd"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(100).Equal(prices["ausd"]))
	require.True(t, math.NewInt(40).Equal(keeper.scoreWeights[consKey(inBandVoter1)]))
	require.True(t, math.NewInt(40).Equal(keeper.scoreWeights[consKey(inBandVoter2)]))
	require.True(t, keeper.scoreWeights[consKey(outOfBandVoter)].IsZero())
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(outOfBandVoter)])
	require.True(t, keeper.scoreWeights[consKey(nonPositiveVoter)].IsZero())
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(nonPositiveVoter)])
	require.Zero(t, keeper.attendedCounts[consKey(nonPositiveVoter)])
}

func TestAggregateOracleVotesMarksNonFunctioningBlockIneligible(t *testing.T) {
	participant := []byte{1}
	absentee := []byte{2}
	votes := []testVote{
		newTestVote(participant, 40, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
			"akrw": math.LegacyNewDec(1000),
		}),
		newTestVote(absentee, 60, map[string]math.LegacyDec{}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	voteTargets := []string{"ausd", "akrw"}

	keeper, _, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	// The participant prices two targets but contributes its 40 power once, so
	// participating power stays below the 1/2 functioning threshold of 100.
	require.Len(t, keeper.scoreOrder, 2)
	require.Zero(t, keeper.eligibleCounts[consKey(participant)])
	require.Zero(t, keeper.eligibleCounts[consKey(absentee)])
}

func TestAggregateOracleVotesTreatsExactlyHalfParticipatingPowerAsFunctioning(t *testing.T) {
	participant := []byte{1}
	absentee := []byte{2}
	votes := []testVote{
		newTestVote(participant, 10, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newAbsentTestVote(absentee, 10),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	voteTargets := []string{"ausd"}

	keeper, _, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	// Participating power is exactly half of total commit power, which the
	// default functioning threshold includes.
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(participant)])
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(participant)])
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(absentee)])
	require.Zero(t, keeper.attendedCounts[consKey(absentee)])
}

func TestAggregateOracleVotesAppliesFunctioningBlockThreshold(t *testing.T) {
	participant := []byte{1}
	absentee := []byte{2}
	voteTargets := []string{"ausd"}

	tests := []struct {
		name        string
		threshold   math.LegacyDec
		functioning bool
	}{
		{
			name:        "default majority threshold grades the block",
			threshold:   oracletypes.DefaultFunctioningBlockThreshold,
			functioning: true,
		},
		{
			name:        "raised threshold spares a fleet below it",
			threshold:   math.LegacyNewDecWithPrec(60, 2),
			functioning: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Participating power is 11 of 20. The price quorum is met either
			// way, so only the functioning threshold moves between cases.
			votes := []testVote{
				newTestVote(participant, 11, map[string]math.LegacyDec{
					"ausd": math.LegacyNewDec(100),
				}),
				newAbsentTestVote(absentee, 9),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = oracletypes.MinVoteThreshold
			params.FunctioningBlockThreshold = tc.threshold

			keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Len(t, prices, 1)
			if !tc.functioning {
				require.Zero(t, keeper.eligibleCounts[consKey(participant)])
				require.Zero(t, keeper.eligibleCounts[consKey(absentee)])
				return
			}
			require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(participant)])
			require.Equal(t, uint64(1), keeper.attendedCounts[consKey(participant)])
			require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(absentee)])
			require.Zero(t, keeper.attendedCounts[consKey(absentee)])
		})
	}
}

func TestAggregateOracleVotesAppliesParticipationThreshold(t *testing.T) {
	anchor := []byte{1}
	reporter := []byte{2}

	tests := []struct {
		name        string
		threshold   math.LegacyDec
		voteTargets []string
		rates       map[string]math.LegacyDec
		attended    bool
	}{
		{
			name:        "zero threshold keeps the single-rate floor",
			threshold:   math.LegacyZeroDec(),
			voteTargets: []string{"aaaa", "abbb", "accc", "addd"},
			rates:       map[string]math.LegacyDec{"aaaa": math.LegacyNewDec(100)},
			attended:    true,
		},
		{
			name:        "report below the floor is eligible only",
			threshold:   oracletypes.MaxParticipationThreshold,
			voteTargets: []string{"aaaa", "abbb", "accc", "addd"},
			rates:       map[string]math.LegacyDec{"aaaa": math.LegacyNewDec(100)},
			attended:    false,
		},
		{
			name:        "report at the floor participates",
			threshold:   oracletypes.MaxParticipationThreshold,
			voteTargets: []string{"aaaa", "abbb", "accc", "addd"},
			rates: map[string]math.LegacyDec{
				"aaaa": math.LegacyNewDec(100),
				"abbb": math.LegacyNewDec(100),
			},
			attended: true,
		},
		{
			name:        "abstentions do not count toward the floor",
			threshold:   oracletypes.MaxParticipationThreshold,
			voteTargets: []string{"aaaa", "abbb", "accc", "addd"},
			rates: map[string]math.LegacyDec{
				"aaaa": math.LegacyNewDec(100),
				"abbb": math.LegacyZeroDec(),
				"accc": math.LegacyZeroDec(),
			},
			attended: false,
		},
		{
			name:        "half threshold over three targets ceils to two",
			threshold:   oracletypes.MaxParticipationThreshold,
			voteTargets: []string{"aaaa", "abbb", "accc"},
			rates:       map[string]math.LegacyDec{"aaaa": math.LegacyNewDec(100)},
			attended:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The anchor prices every target with majority power, so the block
			// is functioning in every case and only the reporter's attendance
			// moves with the threshold.
			anchorRates := make(map[string]math.LegacyDec, len(tc.voteTargets))
			for _, denom := range tc.voteTargets {
				anchorRates[denom] = math.LegacyNewDec(100)
			}
			votes := []testVote{
				newTestVote(anchor, 60, anchorRates),
				newTestVote(reporter, 40, tc.rates),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = oracletypes.MinVoteThreshold
			params.ParticipationThreshold = tc.threshold

			keeper, _, err := processVoteExtensions(t, votes, params, tc.voteTargets)

			require.NoError(t, err)
			require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(anchor)])
			require.Equal(t, uint64(1), keeper.attendedCounts[consKey(anchor)])
			require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(reporter)])
			if tc.attended {
				require.Equal(t, uint64(1), keeper.attendedCounts[consKey(reporter)])
			} else {
				require.Zero(t, keeper.attendedCounts[consKey(reporter)])
			}
		})
	}
}

func TestAggregateOracleVotesExcludesBelowFloorPowerFromFunctioningGate(t *testing.T) {
	fullVoter := []byte{1}
	sparseMajority := []byte{2}
	voteTargets := []string{"aaaa", "abbb", "accc", "addd"}
	votes := []testVote{
		newTestVote(fullVoter, 40, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(100),
			"abbb": math.LegacyNewDec(100),
			"accc": math.LegacyNewDec(100),
			"addd": math.LegacyNewDec(100),
		}),
		newTestVote(sparseMajority, 60, map[string]math.LegacyDec{
			"aaaa": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	params.ParticipationThreshold = oracletypes.MaxParticipationThreshold

	keeper, _, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	// The sparse majority is below the participation floor, so its power must
	// not keep the block functioning: participating power is 40 of 100 and
	// nobody is graded. Counting below-floor power would instead grade every
	// correlated coverage collapse as individual absence.
	require.Zero(t, keeper.eligibleCounts[consKey(fullVoter)])
	require.Zero(t, keeper.eligibleCounts[consKey(sparseMajority)])
}

func TestAggregateOracleVotesTreatsZeroTotalPowerAsNotFunctioning(t *testing.T) {
	pricingVoter := []byte{1}
	absentee := []byte{2}
	votes := []testVote{
		newTestVote(pricingVoter, 0, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newAbsentTestVote(absentee, 0),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	voteTargets := []string{"ausd"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Empty(t, prices)
	// Zero participating power satisfies the multiplied comparison on its own,
	// so only the powerless-commit early return keeps the block from grading
	// attendance.
	require.Len(t, keeper.scoreOrder, 2)
	require.Zero(t, keeper.eligibleCounts[consKey(pricingVoter)])
	require.Zero(t, keeper.eligibleCounts[consKey(absentee)])
}

func TestAggregateOracleVotesRecordsEmptyReportAsEligibleOnly(t *testing.T) {
	participant := []byte{1}
	emptyReporter := []byte{2}
	votes := []testVote{
		newTestVote(participant, 60, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newTestVote(emptyReporter, 40, map[string]math.LegacyDec{}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	voteTargets := []string{"ausd"}

	keeper, prices, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "ausd")
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(participant)])
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(participant)])
	// A valid report carrying no rates is graded like any other non-participant
	// on a functioning block.
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(emptyReporter)])
	require.Zero(t, keeper.attendedCounts[consKey(emptyReporter)])
}

func TestAggregateOracleVotesGradesInvalidReportsEligibleOnFunctioningBlocks(t *testing.T) {
	participant := []byte{1}
	invalidVoter := []byte{2}
	votes := []testVote{
		newTestVote(participant, 60, map[string]math.LegacyDec{
			"ausd": math.LegacyNewDec(100),
		}),
		newAbsentTestVote(invalidVoter, 40),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = oracletypes.MinVoteThreshold
	voteTargets := []string{"ausd"}

	keeper, _, err := processVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(participant)])
	require.Equal(t, uint64(1), keeper.attendedCounts[consKey(participant)])
	require.Equal(t, uint64(1), keeper.eligibleCounts[consKey(invalidVoter)])
	require.Zero(t, keeper.attendedCounts[consKey(invalidVoter)])
}

type testVote struct {
	validator sdk.ConsAddress
	power     int64
	rates     map[string]math.LegacyDec
	// absent carries no vote extension at all, which aggregation sees as an
	// invalid report rather than an empty one.
	absent bool
}

func newTestVote(address []byte, power int64, rates map[string]math.LegacyDec) testVote {
	return testVote{
		validator: sdk.ConsAddress(address),
		power:     power,
		rates:     rates,
	}
}

func newAbsentTestVote(address []byte, power int64) testVote {
	return testVote{
		validator: sdk.ConsAddress(address),
		power:     power,
		absent:    true,
	}
}

// consKey returns the recording keeper's map key for a raw validator address.
func consKey(address []byte) string {
	return sdk.ConsAddress(address).String()
}

func processVoteExtensions(
	t *testing.T,
	votes []testVote,
	params oracletypes.Params,
	voteTargets []string,
) (*recordingOracleKeeper, map[string]math.LegacyDec, error) {
	t.Helper()

	extendedVotes := make([]cmtabci.ExtendedVoteInfo, 0, len(votes))
	for _, vote := range votes {
		var voteExtension []byte
		if !vote.absent {
			encoded, err := codec.EncodeVoteExtension(abcitestutil.NewOracleVoteExtension(t, vote.rates))
			require.NoError(t, err)
			voteExtension = encoded
		}
		extendedVotes = append(extendedVotes, abcitestutil.NewExtendedVoteInfo(vote.validator, vote.power, voteExtension))
	}

	extendedCommit, err := codec.EncodeExtendedCommit(cmtabci.ExtendedCommitInfo{Votes: extendedVotes})
	require.NoError(t, err)

	keeper := newRecordingOracleKeeper(params, voteTargets)
	err = oracle.ProcessVoteExtensions(
		abcitestutil.NewSDKContext(3, 0),
		keeper,
		&cmtabci.RequestFinalizeBlock{
			Height:            3,
			Txs:               [][]byte{extendedCommit},
			DecidedLastCommit: cmtabci.CommitInfo{Votes: make([]cmtabci.VoteInfo, len(extendedVotes))},
		},
	)

	// Assert the prices actually written through the keeper.
	return keeper, keeper.exchangeRate, err
}

type recordingOracleKeeper struct {
	params            oracletypes.Params
	voteTargets       []string
	exchangeRate      map[string]math.LegacyDec
	exchangeRateOrder []string
	scoreWeights      map[string]math.Int
	scoreOrder        []string
	eligibleCounts    map[string]uint64
	attendedCounts    map[string]uint64
}

// newRecordingOracleKeeper sorts voteTargets so callers may list them in any
// order; aggregation requires canonical lexical order.
func newRecordingOracleKeeper(params oracletypes.Params, voteTargets []string) *recordingOracleKeeper {
	denoms := slices.Clone(voteTargets)
	slices.Sort(denoms)

	return &recordingOracleKeeper{
		params:         params,
		voteTargets:    denoms,
		exchangeRate:   make(map[string]math.LegacyDec),
		scoreWeights:   make(map[string]math.Int),
		eligibleCounts: make(map[string]uint64),
		attendedCounts: make(map[string]uint64),
	}
}

func (k *recordingOracleKeeper) GetParams(context.Context) (oracletypes.Params, error) {
	return k.params, nil
}

func (k *recordingOracleKeeper) SetExchangeRateWithEvent(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
	k.exchangeRate[exchangeRate.Denom] = exchangeRate.Rate
	k.exchangeRateOrder = append(k.exchangeRateOrder, exchangeRate.Denom)
	return nil
}

// RecordVoteAccounting mirrors the keeper's composition: attendance is credited
// only when an eligible block is also participated in.
func (k *recordingOracleKeeper) RecordVoteAccounting(
	_ context.Context,
	validator sdk.ConsAddress,
	scoreWeight math.Int,
	eligible bool,
	participated bool,
) error {
	validatorKey := validator.String()
	currentWeight, ok := k.scoreWeights[validatorKey]
	if !ok {
		currentWeight = math.ZeroInt()
	}
	k.scoreWeights[validatorKey] = currentWeight.Add(scoreWeight)
	k.scoreOrder = append(k.scoreOrder, validatorKey)
	if eligible {
		k.eligibleCounts[validatorKey]++
		if participated {
			k.attendedCounts[validatorKey]++
		}
	}
	return nil
}

func (k *recordingOracleKeeper) GetFeeds(context.Context, int64) (oracletypes.FeedSet, error) {
	return oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  slices.Clone(k.voteTargets),
	}, nil
}

func (k *recordingOracleKeeper) AdvanceFeeds(context.Context) error {
	return nil
}
