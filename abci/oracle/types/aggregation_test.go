package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/abci/oracle/types"
	chain "noah/pkg/chain"
)

func TestValidatorMap(t *testing.T) {
	valAddr1 := sdk.ConsAddress([]byte("validator1___________"))
	valAddr2 := sdk.ConsAddress([]byte("validator2___________"))

	tests := []struct {
		name     string
		votes    types.DenomVotes
		expected map[string]math.LegacyDec
	}{
		{
			name: "filters non-positive rates",
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyNewDec(1600), chain.MicroKRWDenom, valAddr1, 100),
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr2, 100),
			},
			expected: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
		},
		{
			name: "includes positive rate with zero power",
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyNewDec(1600), chain.MicroKRWDenom, valAddr1, 0),
			},
			expected: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
		},
		{
			name: "duplicate zero rate does not erase previous positive rate",
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyNewDec(1600), chain.MicroKRWDenom, valAddr1, 100),
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 100),
			},
			expected: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.ValidatorMap())
		})
	}
}

func TestCrossRate(t *testing.T) {
	valAddr1 := sdk.ConsAddress([]byte("validator1___________"))
	valAddr2 := sdk.ConsAddress([]byte("validator2___________"))

	tests := []struct {
		name           string
		referenceRates map[string]math.LegacyDec
		votes          types.DenomVotes
		expected       types.DenomVotes
	}{
		{
			name: "converts matching validator rates and preserves input order",
			referenceRates: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
				string(valAddr2): math.LegacyNewDec(2100),
			},
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyNewDec(100), chain.MicroKRWDenom, valAddr1, 100),
				types.NewDenomVote(math.LegacyNewDec(300), chain.MicroKRWDenom, valAddr2, 200),
			},
			expected: types.DenomVotes{
				types.NewDenomVote(math.LegacyNewDec(16), chain.MicroKRWDenom, valAddr1, 100),
				types.NewDenomVote(math.LegacyNewDec(7), chain.MicroKRWDenom, valAddr2, 200),
			},
		},
		{
			name:           "missing reference rate becomes abstain",
			referenceRates: map[string]math.LegacyDec{},
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyNewDec(100), chain.MicroKRWDenom, valAddr1, 100),
			},
			expected: types.DenomVotes{
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 0),
			},
		},
		{
			name: "zero quote rate becomes abstain",
			referenceRates: map[string]math.LegacyDec{
				string(valAddr1): math.LegacyNewDec(1600),
			},
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 100),
			},
			expected: types.DenomVotes{
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroKRWDenom, valAddr1, 0),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.CrossRate(tc.referenceRates))
		})
	}
}

func TestPower(t *testing.T) {
	valAddr1 := sdk.ConsAddress([]byte("validator1___________"))
	valAddr2 := sdk.ConsAddress([]byte("validator2___________"))

	tests := []struct {
		name     string
		votes    types.DenomVotes
		expected uint64
	}{
		{
			name: "single validator",
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr1, 100),
			},
			expected: 100,
		},
		{
			name: "sums validator power",
			votes: types.DenomVotes{
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr1, 100),
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr2, 200),
				types.NewDenomVote(math.LegacyZeroDec(), chain.MicroSDRDenom, valAddr1, 0),
			},
			expected: 300,
		},
		{
			name:     "empty ballot",
			votes:    types.DenomVotes{},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.Power())
		})
	}
}

func TestWeightedMedian(t *testing.T) {
	valAddr := sdk.ConsAddress([]byte("validator1___________"))
	vote := func(rate int64, power uint64) types.DenomVote {
		return types.NewDenomVote(math.LegacyNewDec(rate), chain.MicroSDRDenom, valAddr, power)
	}

	tests := []struct {
		name     string
		votes    types.DenomVotes
		expected math.LegacyDec
	}{
		{
			name: "high power rate wins",
			votes: types.DenomVotes{
				vote(1, 1),
				vote(2, 1),
				vote(10, 100),
				vote(100000, 1),
			},
			expected: math.LegacyNewDec(10),
		},
		{
			name: "zero power outlier is ignored",
			votes: types.DenomVotes{
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
			votes: types.DenomVotes{
				vote(1, 1),
				vote(2, 100),
				vote(3, 100),
				vote(4, 1),
			},
			expected: math.LegacyNewDec(2),
		},
		{
			name: "unsorted input is sorted before median",
			votes: types.DenomVotes{
				vote(100000, 1),
				vote(10, 100),
				vote(2, 1),
				vote(1, 1),
			},
			expected: math.LegacyNewDec(10),
		},
		{
			name:     "empty ballot",
			votes:    types.DenomVotes{},
			expected: math.LegacyZeroDec(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.votes.WeightedMedian())
		})
	}
}

func TestStandardDeviation(t *testing.T) {
	valAddr := sdk.ConsAddress([]byte("validator1___________"))
	vote := func(rate math.LegacyDec, power uint64) types.DenomVote {
		return types.NewDenomVote(rate, chain.MicroSDRDenom, valAddr, power)
	}
	hugeRate, err := math.LegacyNewDecFromStr("100000000000000000000000000000000000000000000000000000000.0")
	require.NoError(t, err)

	tests := []struct {
		name              string
		votes             types.DenomVotes
		standardDeviation math.LegacyDec
	}{
		{
			name: "wide spread around weighted median",
			votes: types.DenomVotes{
				vote(math.LegacyNewDec(1), 1),
				vote(math.LegacyNewDec(2), 1),
				vote(math.LegacyNewDec(10), 100),
				vote(math.LegacyNewDec(100000), 1),
			},
			standardDeviation: math.LegacyMustNewDecFromStr("49995.000362536252310906"),
		},
		{
			name: "zero power outlier still contributes to deviation",
			votes: types.DenomVotes{
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
			votes: types.DenomVotes{
				vote(math.LegacyNewDec(1), 1),
				vote(math.LegacyNewDec(2), 100),
				vote(math.LegacyNewDec(3), 100),
				vote(math.LegacyNewDec(4), 1),
			},
			standardDeviation: math.LegacyMustNewDecFromStr("1.224744871391589049"),
		},
		{
			name:              "empty ballot",
			votes:             types.DenomVotes{},
			standardDeviation: math.LegacyZeroDec(),
		},
		{
			name: "overflow returns zero",
			votes: types.DenomVotes{
				vote(math.LegacyZeroDec(), 2),
				vote(hugeRate, 1),
			},
			standardDeviation: math.LegacyZeroDec(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.standardDeviation, tc.votes.StandardDeviation(tc.votes.WeightedMedian()))
		})
	}
}
