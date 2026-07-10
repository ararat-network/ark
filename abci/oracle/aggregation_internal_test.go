package oracle

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
)

func TestDenomVotesValidatorMap(t *testing.T) {
	valAddr1 := sdk.ConsAddress([]byte("validator1___________"))
	valAddr2 := sdk.ConsAddress([]byte("validator2___________"))

	tests := []struct {
		name     string
		votes    denomVotes
		expected map[string]math.LegacyDec
	}{
		{
			name: "filters non-positive rates",
			votes: denomVotes{
				newDenomVote(math.LegacyNewDec(1600), chain.MicroKRWDenom, valAddr1, 100),
				newDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr2, 100),
			},
			expected: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
		},
		{
			name: "includes positive rate with zero power",
			votes: denomVotes{
				newDenomVote(math.LegacyNewDec(1600), chain.MicroKRWDenom, valAddr1, 0),
			},
			expected: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
		},
		{
			name: "duplicate zero rate does not erase previous positive rate",
			votes: denomVotes{
				newDenomVote(math.LegacyNewDec(1600), chain.MicroKRWDenom, valAddr1, 100),
				newDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 100),
			},
			expected: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.validatorMap())
		})
	}
}

func TestDenomVotesCrossRate(t *testing.T) {
	valAddr1 := sdk.ConsAddress([]byte("validator1___________"))
	valAddr2 := sdk.ConsAddress([]byte("validator2___________"))

	tests := []struct {
		name           string
		referenceRates map[string]math.LegacyDec
		votes          denomVotes
		expected       denomVotes
	}{
		{
			name: "converts matching validator rates and preserves input order",
			referenceRates: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
				string(valAddr2): math.LegacyNewDec(2100),
			},
			votes: denomVotes{
				newDenomVote(math.LegacyNewDec(100), chain.MicroKRWDenom, valAddr1, 100),
				newDenomVote(math.LegacyNewDec(300), chain.MicroKRWDenom, valAddr2, 200),
			},
			expected: denomVotes{
				newDenomVote(math.LegacyNewDec(16), chain.MicroKRWDenom, valAddr1, 100),
				newDenomVote(math.LegacyNewDec(7), chain.MicroKRWDenom, valAddr2, 200),
			},
		},
		{
			name:           "missing reference rate becomes abstain",
			referenceRates: map[string]math.LegacyDec{},
			votes: denomVotes{
				newDenomVote(math.LegacyNewDec(100), chain.MicroKRWDenom, valAddr1, 100),
			},
			expected: denomVotes{
				newDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 0),
			},
		},
		{
			name: "zero quote rate becomes abstain",
			referenceRates: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
			votes: denomVotes{
				newDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 100),
			},
			expected: denomVotes{
				newDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 0),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.crossRate(tc.referenceRates))
		})
	}
}

func TestDenomVotesOverlapPower(t *testing.T) {
	valAddr1 := sdk.ConsAddress([]byte("validator1___________"))
	valAddr2 := sdk.ConsAddress([]byte("validator2___________"))
	valAddr3 := sdk.ConsAddress([]byte("validator3___________"))

	referenceRates := map[string]math.LegacyDec{
		string(valAddr1): math.LegacyNewDec(1600),
		string(valAddr2): math.LegacyNewDec(2100),
	}
	votes := denomVotes{
		newDenomVote(math.LegacyNewDec(100), chain.MicroKRWDenom, valAddr1, 100),
		newDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr2, 200),
		newDenomVote(math.LegacyNewDec(300), chain.MicroKRWDenom, valAddr3, 300),
	}

	require.Equal(t, uint64(100), votes.overlapPower(referenceRates))
}

func TestDenomVotesPower(t *testing.T) {
	valAddr1 := sdk.ConsAddress([]byte("validator1___________"))
	valAddr2 := sdk.ConsAddress([]byte("validator2___________"))

	tests := []struct {
		name     string
		votes    denomVotes
		expected uint64
	}{
		{
			name: "single validator",
			votes: denomVotes{
				newDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr1, 100),
			},
			expected: 100,
		},
		{
			name: "sums validator power",
			votes: denomVotes{
				newDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr1, 100),
				newDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr2, 200),
				newDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr1, 0),
			},
			expected: 300,
		},
		{
			name:     "empty ballot",
			votes:    denomVotes{},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.power())
		})
	}
}

func TestDenomVotesWeightedMedian(t *testing.T) {
	valAddr := sdk.ConsAddress([]byte("validator1___________"))
	vote := func(rate int64, power uint64) denomVote {
		return newDenomVote(math.LegacyNewDec(rate), chain.MicroSDRDenom, valAddr, power)
	}

	tests := []struct {
		name     string
		votes    denomVotes
		expected math.LegacyDec
	}{
		{
			name: "high power rate wins",
			votes: denomVotes{
				vote(1, 1),
				vote(2, 1),
				vote(10, 100),
				vote(100000, 1),
			},
			expected: math.LegacyNewDec(10),
		},
		{
			name: "zero power outlier is ignored",
			votes: denomVotes{
				vote(1, 1),
				vote(2, 1),
				vote(10, 100),
				vote(100000, 1),
				vote(10000000000, 0),
			},
			expected: math.LegacyNewDec(10),
		},
		{
			name: "tie votes select lower pivot rate",
			votes: denomVotes{
				vote(1, 1),
				vote(2, 100),
				vote(3, 100),
				vote(4, 1),
			},
			expected: math.LegacyNewDec(2),
		},
		{
			name: "unsorted input is sorted before median",
			votes: denomVotes{
				vote(100000, 1),
				vote(10, 100),
				vote(2, 1),
				vote(1, 1),
			},
			expected: math.LegacyNewDec(10),
		},
		{
			name:     "empty ballot",
			votes:    denomVotes{},
			expected: math.LegacyZeroDec(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.weightedMedian())
		})
	}
}

func TestDenomVotesStandardDeviation(t *testing.T) {
	valAddr := sdk.ConsAddress([]byte("validator1___________"))
	vote := func(rate math.LegacyDec, power uint64) denomVote {
		return newDenomVote(rate, chain.MicroSDRDenom, valAddr, power)
	}
	hugeRate, err := math.LegacyNewDecFromStr("100000000000000000000000000000000000000000000000000000000.0")
	require.NoError(t, err)

	tests := []struct {
		name              string
		votes             denomVotes
		standardDeviation math.LegacyDec
	}{
		{
			name: "wide spread around weighted median",
			votes: denomVotes{
				vote(math.LegacyNewDec(1), 1),
				vote(math.LegacyNewDec(2), 1),
				vote(math.LegacyNewDec(10), 100),
				vote(math.LegacyNewDec(100000), 1),
			},
			standardDeviation: math.LegacyMustNewDecFromStr("49995.000362536252310906"),
		},
		{
			name: "zero power outlier still contributes to deviation",
			votes: denomVotes{
				vote(math.LegacyNewDec(1), 1),
				vote(math.LegacyNewDec(2), 1),
				vote(math.LegacyNewDec(10), 100),
				vote(math.LegacyNewDec(100000), 1),
				vote(math.LegacyNewDec(10000000000), 0),
			},
			standardDeviation: math.LegacyMustNewDecFromStr("4472135950.751005519905537611"),
		},
		{
			name: "tie votes",
			votes: denomVotes{
				vote(math.LegacyNewDec(1), 1),
				vote(math.LegacyNewDec(2), 100),
				vote(math.LegacyNewDec(3), 100),
				vote(math.LegacyNewDec(4), 1),
			},
			standardDeviation: math.LegacyMustNewDecFromStr("1.224744871391589049"),
		},
		{
			name:              "empty ballot",
			votes:             denomVotes{},
			standardDeviation: math.LegacyZeroDec(),
		},
		{
			name: "overflow returns zero",
			votes: denomVotes{
				vote(math.LegacyZeroDec(), 2),
				vote(hugeRate, 1),
			},
			standardDeviation: math.LegacyZeroDec(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.standardDeviation, tc.votes.standardDeviation(tc.votes.weightedMedian()))
		})
	}
}
