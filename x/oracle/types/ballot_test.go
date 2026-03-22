package types_test

import (
	"fmt"
	stdMath "math"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/crypto/secp256k1"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

const OracleDecPrecision = 8

func TestToMap(t *testing.T) {
	tests := struct {
		votes   []types.VoteForTally
		isValid []bool
	}{
		[]types.VoteForTally{
			{
				Voter:        sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address()),
				Denom:        core.MicroKRWDenom,
				ExchangeRate: math.LegacyNewDec(1600),
				Power:        100,
			},
			{
				Voter:        sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address()),
				Denom:        core.MicroKRWDenom,
				ExchangeRate: math.LegacyZeroDec(),
				Power:        100,
			},
			{
				Voter:        sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address()),
				Denom:        core.MicroKRWDenom,
				ExchangeRate: math.LegacyNewDec(1500),
				Power:        100,
			},
		},
		[]bool{true, false, true},
	}

	erb := types.ExchangeRateBallot(tests.votes)
	mapData := erb.ToMap()
	for i, vote := range tests.votes {
		exchangeRate, ok := mapData[string(vote.Voter)]
		if tests.isValid[i] {
			require.True(t, ok)
			require.Equal(t, exchangeRate, vote.ExchangeRate)
		} else {
			require.False(t, ok)
		}
	}
}

func TestToCrossRate(t *testing.T) {
	data := []struct {
		base     math.LegacyDec
		quote    math.LegacyDec
		expected math.LegacyDec
	}{
		{
			base:     math.LegacyNewDec(1600),
			quote:    math.LegacyNewDec(100),
			expected: math.LegacyNewDec(16),
		},
		{
			base:     math.LegacyNewDec(0),
			quote:    math.LegacyNewDec(100),
			expected: math.LegacyNewDec(16),
		},
		{
			base:     math.LegacyNewDec(1600),
			quote:    math.LegacyNewDec(0),
			expected: math.LegacyNewDec(16),
		},
	}

	erbBase := types.ExchangeRateBallot{}
	erbQuote := types.ExchangeRateBallot{}
	cb := types.ExchangeRateBallot{}
	for _, data := range data {
		valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())
		if !data.base.IsZero() {
			erbBase = append(erbBase, types.NewVoteForTally(data.base, core.MicroKRWDenom, valAddr, 100))
		}

		erbQuote = append(erbQuote, types.NewVoteForTally(data.quote, core.MicroKRWDenom, valAddr, 100))

		if !data.base.IsZero() && !data.quote.IsZero() {
			cb = append(cb, types.NewVoteForTally(data.base.Quo(data.quote), core.MicroKRWDenom, valAddr, 100))
		} else {
			cb = append(cb, types.NewVoteForTally(math.LegacyZeroDec(), core.MicroKRWDenom, valAddr, 0))
		}
	}

	baseMapBallot := erbBase.ToMap()
	require.Equal(t, cb, erbQuote.ToCrossRate(baseMapBallot))

	sort.Sort(cb)

	require.Equal(t, cb, erbQuote.ToCrossRateWithSort(baseMapBallot))
}

func TestSqrt(t *testing.T) {
	num := math.LegacyNewDecWithPrec(144, 4)
	floatNum, err := strconv.ParseFloat(num.String(), 64)
	require.NoError(t, err)

	floatNum = stdMath.Sqrt(floatNum)
	num, err = math.LegacyNewDecFromStr(fmt.Sprintf("%f", floatNum))
	require.NoError(t, err)

	require.Equal(t, math.LegacyNewDecWithPrec(12, 2), num)
}

func TestPower(t *testing.T) {
	tests := []struct {
		name     string
		powers   []int64
		expected int64
	}{
		{
			name:     "single validator",
			powers:   []int64{100},
			expected: 100,
		},
		{
			name:     "multiple validators",
			powers:   []int64{100, 200, 300},
			expected: 600,
		},
		{
			name:     "zero power validator does not contribute",
			powers:   []int64{100, 200, 0},
			expected: 300,
		},
		{
			name:     "empty ballot",
			powers:   []int64{},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			erb := types.ExchangeRateBallot{}
			for _, power := range tc.powers {
				valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())
				erb = append(erb, types.NewVoteForTally(
					math.LegacyZeroDec(),
					core.MicroSDRDenom,
					valAddr,
					power,
				))
			}
			require.Equal(t, tc.expected, erb.Power())
		})
	}
}

func TestWeightedMedian(t *testing.T) {
	tests := []struct {
		name        string
		inputs      []int64
		weights     []int64
		isValidator []bool
		median      math.LegacyDec
		panic       bool
	}{
		{
			name:        "supermajority one number",
			inputs:      []int64{1, 2, 10, 100000},
			weights:     []int64{1, 1, 100, 1},
			isValidator: []bool{true, true, true, true},
			median:      math.LegacyNewDec(10),
		},
		{
			name:        "fake validator does not change outcome",
			inputs:      []int64{1, 2, 10, 100000, 10000000000},
			weights:     []int64{1, 1, 100, 1, 10000},
			isValidator: []bool{true, true, true, true, false},
			median:      math.LegacyNewDec(10),
		},
		{
			name:        "tie votes",
			inputs:      []int64{1, 2, 3, 4},
			weights:     []int64{1, 100, 100, 1},
			isValidator: []bool{true, true, true, true},
			median:      math.LegacyNewDec(2),
		},
		{
			name:        "no votes",
			inputs:      []int64{},
			weights:     []int64{},
			isValidator: []bool{true, true, true, true},
			median:      math.LegacyNewDec(0),
		},
		{
			name:        "unsorted panics",
			inputs:      []int64{2, 1, 10, 100000},
			weights:     []int64{1, 1, 100, 1},
			isValidator: []bool{true, true, true, true},
			median:      math.LegacyNewDec(10),
			panic:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			erb := types.ExchangeRateBallot{}
			for i, input := range tc.inputs {
				valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())

				power := tc.weights[i]
				if !tc.isValidator[i] {
					power = 0
				}

				vote := types.NewVoteForTally(
					math.LegacyNewDec(input),
					core.MicroSDRDenom,
					valAddr,
					power,
				)

				erb = append(erb, vote)
			}

			if tc.panic {
				require.Panics(t, func() { erb.WeightedMedianWithAssertion() })
			} else {
				require.Equal(t, tc.median, erb.WeightedMedian())
				require.Equal(t, tc.median, erb.WeightedMedianWithAssertion())
			}
		})
	}
}

func TestStandardDeviation(t *testing.T) {
	tests := []struct {
		name              string
		inputs            []float64
		weights           []int64
		isValidator       []bool
		standardDeviation math.LegacyDec
	}{
		{
			name:              "supermajority one number",
			inputs:            []float64{1.0, 2.0, 10.0, 100000.0},
			weights:           []int64{1, 1, 100, 1},
			isValidator:       []bool{true, true, true, true},
			standardDeviation: math.LegacyNewDecWithPrec(4999500036300, OracleDecPrecision),
		},
		{
			name:              "fake validator does not change outcome",
			inputs:            []float64{1.0, 2.0, 10.0, 100000.0, 10000000000},
			weights:           []int64{1, 1, 100, 1, 10000},
			isValidator:       []bool{true, true, true, true, false},
			standardDeviation: math.LegacyNewDecWithPrec(447213595075100600, OracleDecPrecision),
		},
		{
			name:              "tie votes",
			inputs:            []float64{1.0, 2.0, 3.0, 4.0},
			weights:           []int64{1, 100, 100, 1},
			isValidator:       []bool{true, true, true, true},
			standardDeviation: math.LegacyNewDecWithPrec(122474500, OracleDecPrecision),
		},
		{
			name:              "no votes",
			inputs:            []float64{},
			weights:           []int64{},
			isValidator:       []bool{true, true, true, true},
			standardDeviation: math.LegacyNewDecWithPrec(0, 0),
		},
	}

	base := stdMath.Pow10(OracleDecPrecision)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			erb := types.ExchangeRateBallot{}
			for i, input := range tc.inputs {
				valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())

				power := tc.weights[i]
				if !tc.isValidator[i] {
					power = 0
				}

				vote := types.NewVoteForTally(
					math.LegacyNewDecWithPrec(int64(input*base), int64(OracleDecPrecision)),
					core.MicroSDRDenom,
					valAddr,
					power,
				)

				erb = append(erb, vote)
			}

			require.Equal(t, tc.standardDeviation, erb.StandardDeviation(erb.WeightedMedianWithAssertion()))
		})
	}
}

func TestStandardDeviationOverflow(t *testing.T) {
	valAddr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address())
	exchangeRate, err := math.LegacyNewDecFromStr("100000000000000000000000000000000000000000000000000000000.0")
	require.NoError(t, err)

	erb := types.ExchangeRateBallot{types.NewVoteForTally(
		math.LegacyZeroDec(),
		core.MicroSDRDenom,
		valAddr,
		2,
	), types.NewVoteForTally(
		exchangeRate,
		core.MicroSDRDenom,
		valAddr,
		1,
	)}

	require.Equal(t, math.LegacyZeroDec(), erb.StandardDeviation(erb.WeightedMedianWithAssertion()))
}

func TestNewClaim(t *testing.T) {
	power := int64(10)
	weight := int64(11)
	winCount := int64(1)
	addr := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address().Bytes())
	claim := types.NewClaim(power, weight, winCount, addr)
	require.Equal(t, types.Claim{
		Power:     power,
		Weight:    weight,
		WinCount:  winCount,
		Recipient: addr,
	}, claim)
}
