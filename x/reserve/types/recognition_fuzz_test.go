package types_test

import (
	"encoding/binary"
	"math/big"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/reserve/types"
)

// decOne is 1.0 in LegacyDec quanta, the denominator that maps a Dec's raw
// integer onto an exact rational.
var decOne = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

func ratFromDec(d math.LegacyDec) *big.Rat {
	return new(big.Rat).SetFrac(new(big.Int).Set(d.BigInt()), decOne)
}

// solveRecognitionExactly mirrors SolveRecognition in exact rational
// arithmetic: ascending clip thresholds with the index tiebreak, advance
// while the first still-clipped stake fits its share, T = N ÷ D at the break.
// It is the fuzz oracle, so it shares no code with the production solver.
func solveRecognitionExactly(base *big.Rat, stakes []types.RecognitionStake) *big.Rat {
	solve := []int{}
	for i, stake := range stakes {
		if stake.Ratio.IsNil() || !stake.Ratio.IsPositive() {
			continue
		}
		if !stake.Raw.IsNil() && stake.Raw.IsPositive() {
			solve = append(solve, i)
		}
	}

	thresholds := make(map[int]*big.Rat, len(solve))
	for _, index := range solve {
		thresholds[index] = new(big.Rat).Quo(ratFromDec(stakes[index].Raw), ratFromDec(stakes[index].Ratio))
	}
	sort.Slice(solve, func(a, b int) bool {
		if cmp := thresholds[solve[a]].Cmp(thresholds[solve[b]]); cmp != 0 {
			return cmp < 0
		}
		return solve[a] < solve[b]
	})

	numerator, denominator := new(big.Rat).Set(base), new(big.Rat).SetInt64(1)
	for k := 0; k <= len(solve); k++ {
		denominator = new(big.Rat).SetInt64(1)
		numerator = new(big.Rat).Set(base)
		for _, index := range solve[k:] {
			denominator.Sub(denominator, ratFromDec(stakes[index].Ratio))
		}
		for _, index := range solve[:k] {
			numerator.Add(numerator, ratFromDec(stakes[index].Raw))
		}
		if k < len(solve) {
			index := solve[k]
			held := new(big.Rat).Mul(ratFromDec(stakes[index].Raw), denominator)
			permitted := new(big.Rat).Mul(ratFromDec(stakes[index].Ratio), numerator)
			if held.Cmp(permitted) <= 0 {
				continue
			}
		}
		break
	}

	return new(big.Rat).Quo(numerator, denominator)
}

// creditTolerance allows a decimal quantum for division or a split tie, plus whole-unit truncation
// on both the solver and rational reference.
var creditTolerance = math.NewInt(2)

// FuzzSolveRecognition compares the solver with an exact big.Rat oracle. Rounding ties must not
// select an all-unclipped split above base / (1 - sum of ratios).
func FuzzSolveRecognition(f *testing.F) {
	// A rounding-tie pair, a both-breach split, an unbreached correction, a
	// zero base, and a lone breacher.
	f.Add([]byte{2, 1, 1, 1, 1, 1, 1, 1, 1})
	f.Add([]byte{2, 200, 3, 40, 7, 2, 30, 9, 11})
	f.Add([]byte{3, 0, 0, 0, 5, 5, 5, 5, 5, 5, 5, 5, 255, 255})
	f.Add([]byte{1, 255, 255, 255, 255, 255, 255, 255, 255})
	f.Add([]byte{5, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9})

	f.Fuzz(func(t *testing.T, data []byte) {
		stakes, base := recognitionCase(data)
		if stakes == nil {
			t.Skip("not enough data for one stake")
		}

		results, err := types.SolveRecognition(base, stakes)
		require.NoError(t, err, "bounded inputs must solve")
		require.Len(t, results, len(stakes))

		exactTotal := solveRecognitionExactly(
			new(big.Rat).SetInt(base.BigInt()),
			stakes,
		)

		ratioSum := new(big.Rat)
		creditSum := new(big.Rat)
		for i, stake := range stakes {
			result := results[i]
			if stake.Ratio.IsNil() || !stake.Ratio.IsPositive() {
				require.True(t, result.Ceiling.IsZero(), "stake %d holds no share", i)
				require.True(t, result.Credit.IsZero(), "stake %d earns no credit", i)
				continue
			}
			ratioSum.Add(ratioSum, ratFromDec(stake.Ratio))

			require.False(t, result.Credit.GT(result.Ceiling),
				"stake %d: credit %s above ceiling %s", i, result.Credit, result.Ceiling)
			if !stake.Raw.IsNil() {
				require.False(t, result.Credit.GT(stake.Raw.TruncateInt()),
					"stake %d: credit %s above raw %s", i, result.Credit, stake.Raw)
			}

			// Exact counterpart: min(raw, ratio × T) in rationals.
			exactCredit := new(big.Rat).Mul(ratFromDec(stake.Ratio), exactTotal)
			if !stake.Raw.IsNil() && !stake.Raw.IsNegative() {
				if raw := ratFromDec(stake.Raw); raw.Cmp(exactCredit) < 0 {
					exactCredit = raw
				}
			} else {
				exactCredit = new(big.Rat)
			}
			gap := new(big.Rat).Sub(new(big.Rat).SetInt(result.Credit.BigInt()), exactCredit)
			gap.Abs(gap)
			require.True(t, gap.Cmp(new(big.Rat).SetInt(creditTolerance.BigInt())) <= 0,
				"stake %d: credit %s is %s from the exact %s", i, result.Credit, gap.FloatString(6), exactCredit.FloatString(6))

			creditSum.Add(creditSum, new(big.Rat).SetInt(result.Credit.BigInt()))
		}

		// The base bound, the historical failure: base + Σcredits may not
		// exceed base ÷ (1 − Σratios) beyond per-stake rounding slack.
		bound := new(big.Rat).Sub(new(big.Rat).SetInt64(1), ratioSum)
		require.True(t, bound.Sign() > 0, "generator holds the ratio sum below one")
		bound.Quo(new(big.Rat).SetInt(base.BigInt()), bound)
		recognised := new(big.Rat).Add(new(big.Rat).SetInt(base.BigInt()), creditSum)
		slack := new(big.Rat).SetInt64(int64(len(stakes))*2 + 1)
		require.True(t, recognised.Cmp(new(big.Rat).Add(bound, slack)) <= 0,
			"recognised %s breaches the base bound %s", recognised.FloatString(6), bound.FloatString(6))
	})
}

// recognitionCase deterministically maps fuzz bytes to at most six stakes, with total ratios <=
// 0.95 and bounded base/raw values. The denominator margin keeps rounding tolerance meaningful.
func recognitionCase(data []byte) ([]types.RecognitionStake, math.Int) {
	if len(data) < 9 {
		return nil, math.Int{}
	}
	stakeCount := int(data[0])%6 + 1
	data = data[1:]

	take := func(n int) []byte {
		chunk := make([]byte, n)
		copy(chunk, data)
		if len(data) >= n {
			data = data[n:]
		} else {
			data = nil
		}
		return chunk
	}
	takeUint := func() uint64 {
		return binary.LittleEndian.Uint64(take(8))
	}

	base := math.NewIntFromUint64(takeUint() % 1_000_000_000_000_000)

	ratioQuanta := make([]*big.Int, stakeCount)
	quantaSum := new(big.Int)
	for i := range ratioQuanta {
		quanta := new(big.Int).SetUint64(takeUint() % 1_000_000_000_000_000_000)
		ratioQuanta[i] = quanta
		quantaSum.Add(quantaSum, quanta)
	}
	// Rescale to hold Σratios ≤ 0.95: ratio_i ← ratio_i × cap ÷ sum, floored,
	// which cannot raise the sum back above the cap.
	cap95 := new(big.Int).Mul(big.NewInt(95), new(big.Int).Div(decOne, big.NewInt(100)))
	if quantaSum.Cmp(cap95) > 0 {
		for i, quanta := range ratioQuanta {
			scaled := new(big.Int).Mul(quanta, cap95)
			ratioQuanta[i] = scaled.Div(scaled, quantaSum)
		}
	}

	stakes := make([]types.RecognitionStake, stakeCount)
	for i := range stakes {
		whole := takeUint() % 1_000_000_000_000_000
		frac := takeUint() % 1_000_000_000_000_000_000
		raw := new(big.Int).Mul(new(big.Int).SetUint64(whole), decOne)
		raw.Add(raw, new(big.Int).SetUint64(frac))
		stakes[i] = types.RecognitionStake{
			Ratio: math.LegacyNewDecFromBigIntWithPrec(ratioQuanta[i], 18),
			Raw:   math.LegacyNewDecFromBigIntWithPrec(raw, 18),
		}
	}
	return stakes, base
}
