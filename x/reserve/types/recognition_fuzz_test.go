package types_test

import (
	"encoding/binary"
	"math/big"
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

// exactCredit is the reference for one stake: floor(min(raw, ratio × base)) in rationals, sharing
// no code with the production cap. A ratio of whole quanta times a whole balance is exact in
// LegacyDec, so the production credit must match it to the anoah.
func exactCredit(base *big.Rat, stake types.RecognitionStake) *big.Int {
	if stake.Ratio.IsNil() || !stake.Ratio.IsPositive() || stake.Raw.IsNil() || !stake.Raw.IsPositive() {
		return new(big.Int)
	}
	credit := new(big.Rat).Mul(ratFromDec(stake.Ratio), base)
	if raw := ratFromDec(stake.Raw); raw.Cmp(credit) < 0 {
		credit = raw
	}
	return new(big.Int).Quo(credit.Num(), credit.Denom())
}

// FuzzRecognitionCredits compares every credit with the exact rational cap and checks that no
// stake's credit depends on another's: removing any one stake leaves every other credit unchanged.
func FuzzRecognitionCredits(f *testing.F) {
	// A sum at exactly one, a both-clipped pair, an unclipped stake, a zero
	// base, and a lone clipped stake.
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

		results, err := types.RecognitionCredits(base, stakes)
		require.NoError(t, err, "bounded inputs must cap")
		require.Len(t, results, len(stakes))

		baseRat := new(big.Rat).SetInt(base.BigInt())
		creditSum := new(big.Int)
		for i, stake := range stakes {
			want := exactCredit(baseRat, stake)
			require.Zero(t, want.Cmp(results[i].Credit.BigInt()),
				"stake %d: credit %s, exact %s", i, results[i].Credit, want)
			require.False(t, results[i].Credit.GT(results[i].Ceiling),
				"stake %d: credit %s above ceiling %s", i, results[i].Credit, results[i].Ceiling)
			creditSum.Add(creditSum, results[i].Credit.BigInt())
		}
		// The validated sum caps external credit at the balance itself.
		require.True(t, creditSum.Cmp(base.BigInt()) <= 0,
			"credits %s exceed the balance %s", creditSum, base)

		for skip := range stakes {
			rest := append(append([]types.RecognitionStake{}, stakes[:skip]...), stakes[skip+1:]...)
			without, err := types.RecognitionCredits(base, rest)
			require.NoError(t, err)
			for j, k := 0, 0; j < len(stakes); j++ {
				if j == skip {
					continue
				}
				require.True(t, results[j].Credit.Equal(without[k].Credit),
					"removing stake %d moved stake %d from %s to %s", skip, j, results[j].Credit, without[k].Credit)
				k++
			}
		}
	})
}

// recognitionCase deterministically maps fuzz bytes to at most six stakes, with total ratios at most
// one, the validated ceiling, and bounded base/raw values.
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
	// Rescale to hold Σratios ≤ 1: ratio_i ← ratio_i × 1 ÷ sum, floored,
	// which cannot raise the sum back above one.
	if quantaSum.Cmp(decOne) > 0 {
		for i, quanta := range ratioQuanta {
			scaled := new(big.Int).Mul(quanta, decOne)
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
