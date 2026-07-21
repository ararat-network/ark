package oracle

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "ark/abci/oracle/encoding"
	vetypes "ark/abci/ve/types"
	oracletypes "ark/x/oracle/types"
)

func TestBallotAdd(t *testing.T) {
	b := ballot{rates: make([]math.LegacyDec, 3)}
	b.add(tallyVote{validator: 1, rate: math.LegacyNewDec(100), power: 20})
	b.add(tallyVote{validator: 2, rate: math.LegacyNewDec(200), power: 30})

	require.Equal(t, int64(50), b.power)
	require.Len(t, b.votes, 2)
	require.True(t, math.LegacyNewDec(100).Equal(b.rates[1]))
	require.True(t, math.LegacyNewDec(200).Equal(b.rates[2]))
}

func TestBallotCrossRatePowers(t *testing.T) {
	left := makeBallot(
		tallyVote{validator: 1, rate: math.LegacyNewDec(100), power: 20},
		tallyVote{validator: 2, rate: math.LegacyNewDec(200), power: 30},
	)
	right := makeBallot(
		tallyVote{validator: 1, rate: math.LegacyNewDec(1000), power: 20},
		tallyVote{validator: 3, rate: math.LegacyNewDec(3000), power: 40},
	)

	leftPower, rightPower := left.crossRatePowers(right)
	require.Equal(t, int64(20), leftPower)
	require.Equal(t, int64(20), rightPower)

	left = makeBallot(tallyVote{
		validator: 1,
		rate:      math.LegacySmallestDec(),
		power:     20,
	})
	right = makeBallot(tallyVote{
		validator: 1,
		rate:      math.LegacyNewDec(3),
		power:     20,
	})

	leftPower, rightPower = left.crossRatePowers(right)
	require.Zero(t, leftPower)
	require.Equal(t, int64(20), rightPower)
}

func TestBallotCrossRate(t *testing.T) {
	reference := makeBallot(
		tallyVote{validator: 1, rate: math.LegacyNewDec(1600), power: 100},
		tallyVote{validator: 2, rate: math.LegacyNewDec(2100), power: 200},
	)
	target := makeBallot(
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

func TestBallotCrossRateDropsUnsafeQuotient(t *testing.T) {
	reference := makeBallot(tallyVote{
		validator: 1,
		rate:      math.LegacyMustNewDecFromStr("1000000000000000000000000000000000000000000000000000000000000"),
		power:     10,
	})
	target := makeBallot(tallyVote{
		validator: 1,
		rate:      math.LegacySmallestDec(),
		power:     10,
	})

	crossVotes, crossPower := target.appendCrossRates(reference, nil)
	cross := ballot{votes: crossVotes, power: crossPower}

	require.Zero(t, cross.power)
	require.Empty(t, cross.votes)
}

func TestBallotWeightedMedian(t *testing.T) {
	tests := []struct {
		name     string
		votes    []tallyVote
		expected math.LegacyDec
	}{
		{
			name: "high power rate wins",
			votes: []tallyVote{
				{validator: 1, rate: math.LegacyNewDec(1), power: 1},
				{validator: 2, rate: math.LegacyNewDec(2), power: 1},
				{validator: 3, rate: math.LegacyNewDec(10), power: 100},
				{validator: 4, rate: math.LegacyNewDec(100000), power: 1},
			},
			expected: math.LegacyNewDec(10),
		},
		{
			name: "even tie selects lower pivot rate",
			votes: []tallyVote{
				{validator: 1, rate: math.LegacyNewDec(1), power: 1},
				{validator: 2, rate: math.LegacyNewDec(2), power: 100},
				{validator: 3, rate: math.LegacyNewDec(3), power: 100},
				{validator: 4, rate: math.LegacyNewDec(4), power: 1},
			},
			expected: math.LegacyNewDec(2),
		},
		{
			name: "odd total power selects majority pivot rate",
			votes: []tallyVote{
				{validator: 1, rate: math.LegacyNewDec(1), power: 2},
				{validator: 2, rate: math.LegacyNewDec(2), power: 3},
			},
			expected: math.LegacyNewDec(2),
		},
		{
			name: "unsorted input is sorted before median",
			votes: []tallyVote{
				{validator: 1, rate: math.LegacyNewDec(100000), power: 1},
				{validator: 2, rate: math.LegacyNewDec(10), power: 100},
				{validator: 3, rate: math.LegacyNewDec(2), power: 1},
				{validator: 4, rate: math.LegacyNewDec(1), power: 1},
			},
			expected: math.LegacyNewDec(10),
		},
		{
			name:     "empty ballot",
			votes:    nil,
			expected: math.LegacyZeroDec(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, makeBallot(tc.votes...).weightedMedian())
		})
	}
}

func TestSafePositiveQuotient(t *testing.T) {
	tests := []struct {
		name        string
		numerator   math.LegacyDec
		denominator math.LegacyDec
		expected    math.LegacyDec
		ok          bool
	}{
		{
			name:        "ordinary quotient",
			numerator:   math.LegacyNewDec(1600),
			denominator: math.LegacyNewDec(100),
			expected:    math.LegacyNewDec(16),
			ok:          true,
		},
		{
			name:        "large safe quotient",
			numerator:   math.LegacyMustNewDecFromStr("10000000000000000000000000000000000000000"),
			denominator: math.LegacySmallestDec(),
			expected:    math.LegacyMustNewDecFromStr("10000000000000000000000000000000000000000000000000000000000"),
			ok:          true,
		},
		{
			name:        "zero denominator",
			numerator:   math.LegacyOneDec(),
			denominator: math.LegacyZeroDec(),
			expected:    math.LegacyZeroDec(),
			ok:          false,
		},
		{
			name:        "positive quotient that rounds to zero",
			numerator:   math.LegacySmallestDec(),
			denominator: math.LegacyNewDec(3),
			expected:    math.LegacyZeroDec(),
			ok:          false,
		},
		{
			name:        "unsafe quotient",
			numerator:   math.LegacyMustNewDecFromStr("1000000000000000000000000000000000000000000000000000000000000"),
			denominator: math.LegacySmallestDec(),
			expected:    math.LegacyZeroDec(),
			ok:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, ok := safePositiveQuotient(tc.numerator, tc.denominator)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestPositiveQuotientSafelyRepresentable(t *testing.T) {
	decFromRawPower := func(bit uint) math.LegacyDec {
		return math.LegacyNewDecFromBigIntWithPrec(
			new(big.Int).Lsh(big.NewInt(1), bit),
			math.LegacyPrecision,
		)
	}

	tests := []struct {
		name        string
		numerator   math.LegacyDec
		denominator math.LegacyDec
		expected    bool
	}{
		{
			name:        "ordinary ratio",
			numerator:   math.LegacyNewDec(100),
			denominator: math.LegacyNewDec(10),
			expected:    true,
		},
		{
			name:        "lower safe bit boundary",
			numerator:   decFromRawPower(0),
			denominator: decFromRawPower(58),
			expected:    true,
		},
		{
			name:        "below lower safe bit boundary",
			numerator:   decFromRawPower(0),
			denominator: decFromRawPower(59),
			expected:    false,
		},
		{
			name:        "upper safe bit boundary",
			numerator:   decFromRawPower(254),
			denominator: decFromRawPower(0),
			expected:    true,
		},
		{
			name:        "above upper safe bit boundary",
			numerator:   decFromRawPower(255),
			denominator: decFromRawPower(0),
			expected:    false,
		},
		{
			name:        "zero denominator",
			numerator:   math.LegacyOneDec(),
			denominator: math.LegacyZeroDec(),
			expected:    false,
		},
		{
			name:        "negative numerator",
			numerator:   math.LegacyOneDec().Neg(),
			denominator: math.LegacyOneDec(),
			expected:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(
				t,
				tc.expected,
				positiveQuotientSafelyRepresentable(tc.numerator, tc.denominator),
			)
			_, ok := safePositiveQuotient(tc.numerator, tc.denominator)
			require.Equal(t, tc.expected, ok)
		})
	}

	// The policy deliberately rejects some technically representable boundary
	// values instead of reproducing LegacyDec's exact edge rounding rules.
	require.True(t, math.LegacySmallestDec().Quo(math.LegacyOneDec()).IsPositive())
	require.False(t, positiveQuotientSafelyRepresentable(
		math.LegacySmallestDec(),
		math.LegacyOneDec(),
	))
}

func TestWithinSpread(t *testing.T) {
	median := math.LegacyNewDec(100)
	spread := math.LegacyNewDec(1)
	nearMax := math.LegacyNewDecFromBigInt(new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	))

	tests := []struct {
		name   string
		rate   math.LegacyDec
		median math.LegacyDec
		spread math.LegacyDec
		within bool
	}{
		{name: "lower endpoint", rate: math.LegacyNewDec(99), median: median, spread: spread, within: true},
		{name: "upper endpoint", rate: math.LegacyNewDec(101), median: median, spread: spread, within: true},
		{name: "below band", rate: math.LegacyNewDec(98), median: median, spread: spread, within: false},
		{name: "above band", rate: math.LegacyNewDec(102), median: median, spread: spread, within: false},
		{
			name:   "near maximum does not overflow",
			rate:   nearMax,
			median: nearMax,
			spread: nearMax.QuoInt64(100),
			within: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.within, withinSpread(tc.rate, tc.median, tc.spread))
		})
	}
}

func TestReferenceDenomScoreBetterThan(t *testing.T) {
	baseline := referenceDenomScore{
		index:        1,
		denom:        "ubbb",
		priceable:    2,
		overlapPower: math.NewInt(20),
		rawPower:     30,
	}

	tests := []struct {
		name      string
		candidate referenceDenomScore
		other     referenceDenomScore
		better    bool
	}{
		{
			name:      "any candidate beats unset best",
			candidate: baseline,
			other:     referenceDenomScore{index: -1},
			better:    true,
		},
		{
			name: "priceable denom count wins first",
			candidate: referenceDenomScore{
				index:        2,
				denom:        "uzzz",
				priceable:    3,
				overlapPower: math.NewInt(1),
				rawPower:     1,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "qualifying overlap power breaks coverage tie",
			candidate: referenceDenomScore{
				index:        2,
				denom:        "uzzz",
				priceable:    2,
				overlapPower: math.NewInt(21),
				rawPower:     1,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "raw power breaks overlap tie",
			candidate: referenceDenomScore{
				index:        2,
				denom:        "uzzz",
				priceable:    2,
				overlapPower: math.NewInt(20),
				rawPower:     31,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "lexical denom breaks complete tie",
			candidate: referenceDenomScore{
				index:        2,
				denom:        "uaaa",
				priceable:    2,
				overlapPower: math.NewInt(20),
				rawPower:     30,
			},
			other:  baseline,
			better: true,
		},
		{
			name: "lexically later denom loses complete tie",
			candidate: referenceDenomScore{
				index:        2,
				denom:        "uzzz",
				priceable:    2,
				overlapPower: math.NewInt(20),
				rawPower:     30,
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

func TestSelectReferenceUsesSafeCrossPower(t *testing.T) {
	ballots := []ballot{
		makeBallot(tallyVote{
			validator: 1,
			rate:      math.LegacyOneDec(),
			power:     10,
		}),
		makeBallot(tallyVote{
			validator: 1,
			rate: math.LegacyNewDecFromBigInt(
				new(big.Int).Lsh(big.NewInt(1), 60),
			),
			power: 10,
		}),
	}

	selection := selectReference(
		[]int{0, 1},
		[]string{"uaaa", "uzzz"},
		ballots,
		10,
	)

	require.Equal(t, 1, selection.score.index)
	require.Len(t, selection.tallies, 2)
}

func TestEvaluateReferenceCandidateSkipsUnsafeFinalPrice(t *testing.T) {
	nearMax := math.LegacyNewDecFromBigInt(new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 256),
		big.NewInt(1),
	))
	ballots := []ballot{
		makeBallot(
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
		makeBallot(tallyVote{
			validator: 1,
			rate:      math.LegacyNewDec(4),
			power:     34,
		}),
	}
	crossVotes, crossPower := ballots[1].appendCrossRates(ballots[0], nil)
	cross := ballot{votes: crossVotes, power: crossPower}
	require.Equal(t, int64(34), cross.power)
	require.True(t, math.LegacyNewDecWithPrec(25, 2).Equal(cross.weightedMedian()))
	_, finalPriceOK := safePositiveQuotient(ballots[0].weightedMedian(), cross.weightedMedian())
	require.False(t, finalPriceOK)

	upperBound := referenceUpperBound{
		score: referenceDenomScore{
			index:        0,
			denom:        "uaaa",
			priceable:    2,
			overlapPower: math.NewInt(34),
			rawPower:     100,
		},
		qualified: []bool{true, true},
	}

	workspace := referenceWorkspace{}
	selection := evaluateReferenceCandidate(upperBound, []int{0, 1}, ballots, 34, &workspace, 3)

	require.Equal(t, 1, selection.score.priceable)
	require.Len(t, selection.tallies, 1)
	require.Equal(t, 0, selection.tallies[0].targetIndex)

	selection = selectReference([]int{0, 1}, []string{"uaaa", "uzzz"}, ballots, 34)
	require.Equal(t, 1, selection.score.index)
	require.Len(t, selection.tallies, 2)
}

func TestComputePricesAndScoresSupportsLegalMaximumPowerAcrossTargets(t *testing.T) {
	const targetCount = oracletypes.MaxVoteTargets

	scores := []validatorScore{{rewardWeight: math.ZeroInt()}}
	selection := referenceSelection{tallies: make([]pricedTally, targetCount)}
	targetDenoms := make([]string, targetCount)
	for targetIndex := range targetCount {
		selection.tallies[targetIndex] = pricedTally{
			targetIndex: targetIndex,
			votes: []tallyVote{
				{validator: 0, rate: math.LegacyOneDec(), power: cmttypes.MaxTotalVotingPower},
			},
			median: math.LegacyOneDec(),
			price:  math.LegacyOneDec(),
		}
		targetDenoms[targetIndex] = fmt.Sprintf("u%03d", targetIndex)
	}

	prices := computePricesAndScores(
		selection,
		targetDenoms,
		math.LegacyZeroDec(),
		scores,
	)

	require.NotNil(t, prices)
	expectedWeight := math.NewInt(cmttypes.MaxTotalVotingPower).MulRaw(targetCount)
	require.True(t, expectedWeight.Equal(scores[0].rewardWeight))
}

func TestAggregateOracleVotesReturnsValidatorReports(t *testing.T) {
	validator := sdk.ConsAddress([]byte{1})
	reportedRate := math.LegacyNewDec(100)
	params := oracletypes.DefaultParams()
	voteTargets := []string{"uaaa"}

	result, err := aggregateOracleVotes([]Vote{
		{
			Validator: cometabci.Validator{Address: validator, Power: 1},
			OracleVoteExtension: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uaaa": mustEncodeRate(t, reportedRate),
			}},
		},
	}, params, voteTargets)
	require.NoError(t, err)
	require.Len(t, result.ValidatorReports, 1)
	require.True(t, reportedRate.Equal(result.ValidatorReports[0].Rates["uaaa"]))

	_, err = aggregateOracleVotes([]Vote{
		{
			Validator: cometabci.Validator{Address: validator, Power: 1},
			OracleVoteExtension: vetypes.OracleVoteExtension{Rates: map[string][]byte{
				"uaaa": []byte("malformed"),
			}},
		},
	}, params, voteTargets)
	require.Error(t, err)
}

func makeBallot(votes ...tallyVote) ballot {
	validatorCount := 0
	for _, vote := range votes {
		if vote.validator >= validatorCount {
			validatorCount = vote.validator + 1
		}
	}

	b := ballot{rates: make([]math.LegacyDec, validatorCount)}
	for _, vote := range votes {
		b.add(vote)
	}

	return b
}

func mustEncodeRate(t *testing.T, rate math.LegacyDec) []byte {
	t.Helper()

	bz, err := oracleencoding.EncodeRate(rate)
	require.NoError(t, err)

	return bz
}
