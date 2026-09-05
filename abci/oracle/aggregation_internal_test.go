package oracle

import (
	"fmt"
	"math/big"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/math"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func TestAggregateOracleVotesWithNoTargetsIsNotFunctioning(t *testing.T) {
	votes := []Vote{
		{Validator: cmtabci.Validator{Address: []byte("validator1"), Power: 10}},
		{Validator: cmtabci.Validator{Address: []byte("validator2"), Power: 20}},
	}

	result := aggregateOracleVotes(votes, oracletypes.DefaultParams(), []string{})

	require.Empty(t, result.prices)
	require.False(t, result.functioningBlock)
	require.Len(t, result.scores, len(votes))
	for _, score := range result.scores {
		require.True(t, score.rewardWeight.IsZero())
		require.False(t, score.participated)
	}
}

func TestBallotAdd(t *testing.T) {
	b := ballot{rates: make([]math.LegacyDec, 3)}
	b.add(tallyVote{validator: 1, rate: math.LegacyNewDec(100), power: 20})
	b.add(tallyVote{validator: 2, rate: math.LegacyNewDec(200), power: 30})

	require.Equal(t, int64(50), b.power)
	require.Len(t, b.votes, 2)
	require.True(t, math.LegacyNewDec(100).Equal(b.rates[1]))
	require.True(t, math.LegacyNewDec(200).Equal(b.rates[2]))
}

func TestBallotOverlapPower(t *testing.T) {
	left := makeBallot(4,
		tallyVote{validator: 1, rate: math.LegacyNewDec(100), power: 20},
		tallyVote{validator: 2, rate: math.LegacyNewDec(200), power: 30},
	)
	right := makeBallot(4,
		tallyVote{validator: 1, rate: math.LegacyNewDec(1000), power: 20},
		tallyVote{validator: 3, rate: math.LegacyNewDec(3000), power: 40},
	)

	require.Equal(t, int64(20), left.overlapPower(right))

	left = makeBallot(2, tallyVote{
		validator: 1,
		rate:      math.LegacySmallestDec(),
		power:     20,
	})
	right = makeBallot(2, tallyVote{
		validator: 1,
		rate:      math.LegacyNewDec(3),
		power:     20,
	})

	// Shared raw power is independent of the submitted rate values.
	require.Equal(t, int64(20), left.overlapPower(right))
}

func TestBallotCrossRate(t *testing.T) {
	reference := makeBallot(4,
		tallyVote{validator: 1, rate: math.LegacyNewDec(1600), power: 100},
		tallyVote{validator: 2, rate: math.LegacyNewDec(2100), power: 200},
	)
	target := makeBallot(4,
		tallyVote{validator: 1, rate: math.LegacyNewDec(100), power: 100},
		tallyVote{validator: 2, rate: math.LegacyNewDec(300), power: 200},
		tallyVote{validator: 3, rate: math.LegacyNewDec(500), power: 300},
	)

	crossVotes, crossPower := target.appendCrossRates(reference, nil)
	cross := ballot{votes: crossVotes, power: crossPower}

	require.Equal(t, int64(300), cross.power)
	require.Len(t, cross.votes, 2)
	require.Equal(t, 1, cross.votes[0].validator)
	require.True(t, math.LegacyNewDec(16).Equal(cross.votes[0].rate))
	require.Equal(t, 2, cross.votes[1].validator)
	require.True(t, math.LegacyNewDec(7).Equal(cross.votes[1].rate))
}

func TestBallotCrossRateUsesLegacyDecPrecision(t *testing.T) {
	reference := makeBallot(1, tallyVote{
		validator: 0,
		rate:      math.LegacyOneDec(),
		power:     1,
	})
	target := makeBallot(1, tallyVote{
		validator: 0,
		rate:      math.LegacyNewDec(3),
		power:     1,
	})

	crossVotes, crossPower := target.appendCrossRates(reference, nil)

	require.Equal(t, int64(1), crossPower)
	require.Len(t, crossVotes, 1)
	require.True(t, math.LegacyMustNewDecFromStr("0.333333333333333333").Equal(crossVotes[0].rate))
}

func TestBallotCrossRateSkipsInvalidQuotients(t *testing.T) {
	tests := []struct {
		name          string
		referenceRate math.LegacyDec
		targetRate    math.LegacyDec
	}{
		{
			name:          "overflow",
			referenceRate: math.LegacyMustNewDecFromStr("1000000000000000000000000000000000000000000000000000000000000"),
			targetRate:    math.LegacySmallestDec(),
		},
		{
			name:          "rounds to zero",
			referenceRate: math.LegacySmallestDec(),
			targetRate:    math.LegacyNewDec(3),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reference := makeBallot(1, tallyVote{
				validator: 0,
				rate:      tc.referenceRate,
				power:     10,
			})
			target := makeBallot(1, tallyVote{
				validator: 0,
				rate:      tc.targetRate,
				power:     10,
			})

			crossVotes, crossPower := target.appendCrossRates(reference, nil)

			require.Empty(t, crossVotes)
			require.Zero(t, crossPower)
		})
	}
}

func TestBallotWeightedMedian(t *testing.T) {
	tests := []struct {
		name     string
		ballot   ballot
		expected math.LegacyDec
	}{
		{
			name:     "empty",
			ballot:   ballot{},
			expected: math.LegacyZeroDec(),
		},
		{
			name: "even power selects lower median",
			ballot: ballot{
				votes: []tallyVote{
					{validator: 1, rate: math.LegacyNewDec(2), power: 1},
					{validator: 0, rate: math.LegacyOneDec(), power: 1},
				},
				power: 2,
			},
			expected: math.LegacyOneDec(),
		},
		{
			name: "odd power reaches ceiling",
			ballot: ballot{
				votes: []tallyVote{
					{validator: 0, rate: math.LegacyOneDec(), power: 1},
					{validator: 1, rate: math.LegacyNewDec(2), power: 2},
				},
				power: 3,
			},
			expected: math.LegacyNewDec(2),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, tc.expected.Equal(tc.ballot.weightedMedian()))
		})
	}
}

func TestComputePricesAndScoresRewardBand(t *testing.T) {
	median := math.LegacyNewDec(100)

	tests := []struct {
		name          string
		rate          math.LegacyDec
		median        math.LegacyDec
		expectedPrice math.LegacyDec
		rewarded      bool
	}{
		{
			name:          "lower endpoint",
			rate:          math.LegacyNewDec(99),
			median:        median,
			expectedPrice: median,
			rewarded:      true,
		},
		{
			name:          "upper endpoint",
			rate:          math.LegacyNewDec(101),
			median:        median,
			expectedPrice: median,
			rewarded:      true,
		},
		{
			name:          "below band",
			rate:          math.LegacyNewDec(98),
			median:        median,
			expectedPrice: median,
			rewarded:      false,
		},
		{
			name:          "above band",
			rate:          math.LegacyNewDec(102),
			median:        median,
			expectedPrice: median,
			rewarded:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scores := []validatorScore{{votingPower: 1, rewardWeight: math.ZeroInt()}}
			prices := computePricesAndScores(
				[]pricedTally{{
					targetIndex: 0,
					votes:       []tallyVote{{validator: 0, rate: tc.rate, power: 1}},
					median:      tc.median,
					price:       tc.expectedPrice,
				}},
				[]string{"ausd"},
				math.LegacyNewDecWithPrec(2, 2),
				scores,
			)

			require.Equal(t, tc.expectedPrice, prices["ausd"])
			if tc.rewarded {
				require.True(t, math.OneInt().Equal(scores[0].rewardWeight))
			} else {
				require.True(t, scores[0].rewardWeight.IsZero())
			}
		})
	}
}

func TestComputePricesAndScoresPublishesPriceWhenRewardBandUnrepresentable(t *testing.T) {
	nearMax := math.LegacyNewDecFromBigInt(new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	))
	scores := []validatorScore{{votingPower: 1, rewardWeight: math.ZeroInt()}}

	prices := computePricesAndScores(
		[]pricedTally{{
			targetIndex: 0,
			votes:       []tallyVote{{validator: 0, rate: nearMax, power: 1}},
			median:      nearMax,
			price:       nearMax,
		}},
		[]string{"ausd"},
		math.LegacyNewDecWithPrec(2, 2),
		scores,
	)

	require.Len(t, prices, 1)
	require.True(t, nearMax.Equal(prices["ausd"]))
	require.True(t, scores[0].rewardWeight.IsZero())
}

func TestReferenceScoreBetterThan(t *testing.T) {
	baseline := referenceScore{
		targetIndex:      1,
		qualifiedTargets: 2,
		overlapPower:     math.NewInt(20),
		rawPower:         30,
	}

	tests := []struct {
		name      string
		candidate referenceScore
		other     referenceScore
		better    bool
	}{
		{
			name: "qualified target count wins first",
			candidate: referenceScore{
				targetIndex:      2,
				qualifiedTargets: 3,
				overlapPower:     math.NewInt(1),
				rawPower:         1,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "qualifying overlap power breaks coverage tie",
			candidate: referenceScore{
				targetIndex:      2,
				qualifiedTargets: 2,
				overlapPower:     math.NewInt(21),
				rawPower:         1,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "raw power breaks overlap tie",
			candidate: referenceScore{
				targetIndex:      2,
				qualifiedTargets: 2,
				overlapPower:     math.NewInt(20),
				rawPower:         31,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "lower canonical target index breaks complete tie",
			candidate: referenceScore{
				targetIndex:      0,
				qualifiedTargets: 2,
				overlapPower:     math.NewInt(20),
				rawPower:         30,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "higher canonical target index loses complete tie",
			candidate: referenceScore{
				targetIndex:      2,
				qualifiedTargets: 2,
				overlapPower:     math.NewInt(20),
				rawPower:         30,
			},
			other:  baseline,
			better: false,
		},
		{
			name:      "exact tie keeps incumbent",
			candidate: baseline,
			other:     baseline,
			better:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.better, tc.candidate.betterThan(tc.other))
		})
	}
}

func TestScoreReferencesMatchesPairwise(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	for trial := range 100 {
		targetCount := random.Intn(8) + 1
		validatorCount := random.Intn(10) + 1
		passing := make([]int, 0, targetCount)
		ballots := make([]ballot, targetCount)
		validatorPowers := make([]int64, validatorCount)
		baseSupport := make([]bool, validatorCount)
		var totalPower int64
		for validatorIndex := range validatorCount {
			validatorPowers[validatorIndex] = random.Int63n(10) + 1
			totalPower += validatorPowers[validatorIndex]
			baseSupport[validatorIndex] = validatorIndex == 0 || random.Intn(3) != 0
		}

		uniformSupport := trial%2 == 0
		for targetIndex := range targetCount {
			ballots[targetIndex].rates = make([]math.LegacyDec, validatorCount)
			for validatorIndex := range validatorCount {
				reported := baseSupport[validatorIndex]
				if !uniformSupport {
					reported = validatorIndex == 0 || random.Intn(3) != 0
				}
				if !reported {
					continue
				}
				ballots[targetIndex].add(tallyVote{
					validator: validatorIndex,
					rate: math.LegacyNewDec(
						int64(targetIndex+1)*100 + int64(validatorIndex+1),
					),
					power: validatorPowers[validatorIndex],
				})
			}
		}

		thresholdPower := random.Int63n(totalPower) + 1
		for targetIndex := range ballots {
			if ballots[targetIndex].power >= thresholdPower {
				passing = append(passing, targetIndex)
			}
		}
		if len(passing) == 0 {
			continue
		}

		expected := pairwiseReferenceScores(passing, ballots, thresholdPower)
		actual := scoreReferences(passing, ballots, thresholdPower)
		requireReferenceScoresEqual(t, expected, actual, trial)
	}
}

func TestScoreReferencesSupportsLegalMaximumPower(t *testing.T) {
	const targetCount = oracletypes.MaxFeeds

	passing := make([]int, targetCount)
	ballots := make([]ballot, targetCount)
	for targetIndex := range targetCount {
		passing[targetIndex] = targetIndex
		ballots[targetIndex] = makeBallot(1, tallyVote{
			validator: 0,
			rate:      math.LegacyOneDec(),
			power:     cmttypes.MaxTotalVotingPower,
		})
	}

	scores := scoreReferences(
		passing,
		ballots,
		cmttypes.MaxTotalVotingPower,
	)

	require.Len(t, scores, targetCount)
	require.Equal(t, targetCount, scores[0].qualifiedTargets)
	expectedOverlap := math.NewInt(cmttypes.MaxTotalVotingPower).MulRaw(int64(targetCount - 1))
	require.True(t, expectedOverlap.Equal(scores[0].overlapPower))
}

func TestScoreReferencesSupportsMaximumPowerWithMixedSupport(t *testing.T) {
	const targetCount = oracletypes.MaxFeeds

	halfPower := cmttypes.MaxTotalVotingPower / 2
	passing := make([]int, targetCount)
	ballots := make([]ballot, targetCount)
	for targetIndex := range targetCount {
		passing[targetIndex] = targetIndex
		if targetIndex%2 == 0 {
			ballots[targetIndex] = makeBallot(2,
				tallyVote{validator: 0, rate: math.LegacyOneDec(), power: halfPower},
				tallyVote{
					validator: 1,
					rate:      math.LegacyOneDec(),
					power:     cmttypes.MaxTotalVotingPower - halfPower,
				},
			)
			continue
		}
		ballots[targetIndex] = makeBallot(2, tallyVote{
			validator: 0,
			rate:      math.LegacyOneDec(),
			power:     halfPower,
		})
	}

	scores := scoreReferences(passing, ballots, halfPower)

	require.Len(t, scores, targetCount)
	require.Equal(t, targetCount, scores[0].qualifiedTargets)
	require.Equal(t, targetCount, scores[1].qualifiedTargets)
	expectedEvenOverlap := math.NewInt(cmttypes.MaxTotalVotingPower).
		MulRaw(targetCount/2 - 1).
		Add(math.NewInt(halfPower).MulRaw(targetCount / 2))
	expectedOddOverlap := math.NewInt(halfPower).MulRaw(targetCount - 1)
	require.True(t, expectedEvenOverlap.Equal(scores[0].overlapPower))
	require.True(t, expectedOddOverlap.Equal(scores[1].overlapPower))
}

func TestSelectReferenceUniformFastPathMatchesPairwiseFallback(t *testing.T) {
	ballots := []ballot{
		makeBallot(2,
			tallyVote{validator: 0, rate: math.LegacyNewDec(100), power: 40},
			tallyVote{validator: 1, rate: math.LegacyNewDec(110), power: 60},
		),
		makeBallot(2,
			tallyVote{validator: 0, rate: math.LegacyNewDec(200), power: 40},
			tallyVote{validator: 1, rate: math.LegacyNewDec(220), power: 60},
		),
		makeBallot(2,
			tallyVote{validator: 0, rate: math.LegacyNewDec(400), power: 40},
			tallyVote{validator: 1, rate: math.LegacyNewDec(440), power: 60},
		),
	}
	passing := []int{0, 1, 2}
	require.True(t, passingBallotsShareSupport(passing, ballots))

	fallbackBallots := cloneBallots(ballots)
	slices.Reverse(fallbackBallots[1].votes)
	require.False(t, passingBallotsShareSupport(passing, fallbackBallots))

	fastTallies := selectReference(passing, ballots, 50)
	fallbackTallies := selectReference(passing, fallbackBallots, 50)
	requirePricedTalliesEqual(t, fallbackTallies, fastTallies)
}

func TestSelectReferenceComputesCrossRate(t *testing.T) {
	targetRate := math.LegacyNewDec(4)
	ballots := []ballot{
		makeBallot(2, tallyVote{
			validator: 1,
			rate:      math.LegacyOneDec(),
			power:     10,
		}),
		makeBallot(2, tallyVote{
			validator: 1,
			rate:      targetRate,
			power:     10,
		}),
	}

	tallies := selectReference(
		[]int{0, 1},
		ballots,
		10,
	)

	require.Len(t, tallies, 2)
	require.Equal(t, 0, tallies[0].targetIndex)
	require.True(t, math.LegacyNewDecWithPrec(25, 2).Equal(tallies[1].median))
	require.True(t, targetRate.Equal(tallies[1].price))
}

func TestSelectReferenceSkipsUnrepresentableFinalPrice(t *testing.T) {
	nearMax := math.LegacyNewDecFromBigInt(new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	))
	ballots := []ballot{
		makeBallot(2,
			tallyVote{
				validator: 0,
				rate:      nearMax,
				power:     66,
			},
			tallyVote{
				validator: 1,
				rate:      math.LegacyOneDec(),
				power:     34,
			},
		),
		makeBallot(2, tallyVote{
			validator: 1,
			rate:      math.LegacyNewDec(4),
			power:     34,
		}),
	}

	tallies := selectReference([]int{0, 1}, ballots, 34)

	require.Len(t, tallies, 1)
	require.Equal(t, 0, tallies[0].targetIndex)
}

func TestComputePricesAndScoresSupportsLegalMaximumPowerAcrossTargets(t *testing.T) {
	const targetCount = oracletypes.MaxFeeds

	scores := []validatorScore{{
		votingPower:  cmttypes.MaxTotalVotingPower,
		rewardWeight: math.ZeroInt(),
	}}
	tallies := make([]pricedTally, targetCount)
	targetDenoms := make([]string, targetCount)
	for targetIndex := range targetCount {
		tallies[targetIndex] = pricedTally{
			targetIndex: targetIndex,
			votes: []tallyVote{
				{
					validator: 0,
					rate:      math.LegacyOneDec(),
					power:     cmttypes.MaxTotalVotingPower,
				},
			},
			median: math.LegacyOneDec(),
			price:  math.LegacyOneDec(),
		}
		targetDenoms[targetIndex] = fmt.Sprintf("a%03d", targetIndex)
	}

	prices := computePricesAndScores(
		tallies,
		targetDenoms,
		math.LegacyZeroDec(),
		scores,
	)

	require.NotNil(t, prices)
	expectedWeight := math.NewInt(cmttypes.MaxTotalVotingPower).MulRaw(targetCount)
	require.True(t, expectedWeight.Equal(scores[0].rewardWeight))
}

// makeBallot builds a ballot over validatorCount validators, matching the
// production invariant that every ballot in a tally shares the same rates
// width.
func makeBallot(validatorCount int, votes ...tallyVote) ballot {
	b := ballot{rates: make([]math.LegacyDec, validatorCount)}
	for _, vote := range votes {
		b.add(vote)
	}

	return b
}

func pairwiseReferenceScores(
	passing []int,
	ballots []ballot,
	thresholdPower int64,
) []referenceScore {
	scores := make([]referenceScore, len(passing))
	for i, targetIndex := range passing {
		scores[i] = referenceScore{
			targetIndex:      targetIndex,
			qualifiedTargets: 1,
			overlapPower:     math.ZeroInt(),
			rawPower:         ballots[targetIndex].power,
		}
	}

	for i := range passing {
		for j := i + 1; j < len(passing); j++ {
			overlapPower := ballots[passing[i]].overlapPower(ballots[passing[j]])
			if overlapPower >= thresholdPower {
				scores[i].qualifiedTargets++
				scores[i].overlapPower = scores[i].overlapPower.AddRaw(overlapPower)
				scores[j].qualifiedTargets++
				scores[j].overlapPower = scores[j].overlapPower.AddRaw(overlapPower)
			}
		}
	}

	return scores
}

func requireReferenceScoresEqual(
	t *testing.T,
	expected,
	actual []referenceScore,
	trial int,
) {
	t.Helper()
	require.Len(t, actual, len(expected), "trial %d", trial)
	for i := range expected {
		require.Equal(t, expected[i].targetIndex, actual[i].targetIndex, "trial %d candidate %d", trial, i)
		require.Equal(t, expected[i].qualifiedTargets, actual[i].qualifiedTargets, "trial %d candidate %d", trial, i)
		require.True(t, expected[i].overlapPower.Equal(actual[i].overlapPower), "trial %d candidate %d", trial, i)
		require.Equal(t, expected[i].rawPower, actual[i].rawPower, "trial %d candidate %d", trial, i)
	}
}

func cloneBallots(source []ballot) []ballot {
	cloned := make([]ballot, len(source))
	for i := range source {
		cloned[i] = source[i]
		cloned[i].votes = slices.Clone(source[i].votes)
		cloned[i].rates = slices.Clone(source[i].rates)
	}
	return cloned
}

func requirePricedTalliesEqual(t *testing.T, expected, actual []pricedTally) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].targetIndex, actual[i].targetIndex)
		require.True(t, expected[i].median.Equal(actual[i].median))
		require.True(t, expected[i].price.Equal(actual[i].price))
		require.Equal(t, expected[i].votes, actual[i].votes)
	}
}
