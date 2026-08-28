package types

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// MaxRecognitionRateAge bounds the staleness window one entry may state. It
// is a domain cap with orders of magnitude of headroom over any real
// publication gap, because the fold this window feeds is halt-class
// arithmetic running every block through Treasury settlement. It makes an
// absurd window impossible, not a sensible one mandatory.
const MaxRecognitionRateAge = 30 * 24 * time.Hour

// Validate validates one eligibility entry.
func (entry EligibilityEntry) Validate() error {
	// The external-symbol rule makes an Ark-issued denomination unlistable
	// here by shape alone, and its prefix rule also excludes the NOAH
	// numeraire.
	if err := chain.ValidateExternalDenom(entry.Denom); err != nil {
		return fmt.Errorf("invalid eligibility denom: %w", err)
	}
	// Both figures are strictly positive because an entry exists to grant
	// credit: one that grants none is indistinguishable from no entry at all.
	// Refusing it makes every stored entry a live feed-guard claim.
	if entry.HaircutFactor.IsNil() ||
		!entry.HaircutFactor.IsPositive() ||
		entry.HaircutFactor.GT(math.LegacyOneDec()) {
		return errors.New("eligibility haircut factor must be greater than zero and at most one")
	}
	if entry.RecognitionCapRatio.IsNil() ||
		!entry.RecognitionCapRatio.IsPositive() ||
		entry.RecognitionCapRatio.GT(math.LegacyOneDec()) {
		return errors.New("eligibility recognition cap ratio must be greater than zero and at most one")
	}
	// Required rather than defaulted: an entry states its own tolerance rather
	// than inheriting the Oracle's conversion-grade default.
	if entry.MaxRateAge <= 0 || entry.MaxRateAge > MaxRecognitionRateAge {
		return fmt.Errorf(
			"eligibility max rate age must be greater than zero and at most %s, is %s",
			MaxRecognitionRateAge,
			entry.MaxRateAge,
		)
	}
	return nil
}

// GrantsCredit reports whether this entry can produce a nonzero recognition
// credit; under a validated policy every stored entry does, since Validate
// refuses a zero haircut and a zero cap ratio. It guards what the committee
// may burn, and reads policy alone: an entry whose credit is currently zeroed
// by a dark feed still grants credit.
func (entry EligibilityEntry) GrantsCredit() bool {
	return entry.HaircutFactor.IsPositive() && entry.RecognitionCapRatio.IsPositive()
}

// ValidateRecognitionPolicy validates a complete eligibility set: every entry
// valid, one entry per asset, cap ratios summing strictly below one. The
// entry count is deliberately unbounded — a cap would only ever bind on
// governance, the sole writer, and would size nothing.
func ValidateRecognitionPolicy(entries []EligibilityEntry) error {
	seen := make(map[string]struct{}, len(entries))
	ratioSum := math.LegacyZeroDec()
	for _, entry := range entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[entry.Denom]; duplicate {
			return fmt.Errorf("duplicate eligibility entry for %s", entry.Denom)
		}
		seen[entry.Denom] = struct{}{}
		ratioSum = ratioSum.Add(entry.RecognitionCapRatio)
	}
	// The sum rule is existence, not taste: the total solves
	// T = base + Σ min(raw, ratio × T), so were every capped asset to breach
	// at once, T = base ÷ (1 − Σratios), and at a sum of one the caps admit
	// everything.
	if ratioSum.GTE(math.LegacyOneDec()) {
		return fmt.Errorf(
			"recognition cap ratios must sum strictly below one, got %s: "+
				"at one the shrinking-denominator solve is unbounded and the caps admit everything",
			ratioSum,
		)
	}
	return nil
}

// RawCredit values gross base units of the entry's asset in anoah at the
// set's rate and applies the haircut, deliberately un-clipped: it is one
// asset's input to SolveRecognition, which owns the cap. The haircut lands
// before the conversion, so the single division falls on a quantity whose
// bounds are whole base units. The caller owes a rate that is present and
// positive; a missing or zero rate is an error here.
func (entry EligibilityEntry) RawCredit(rates oracletypes.RateSet, gross math.Int) (math.LegacyDec, error) {
	if gross.IsNil() || !gross.IsPositive() || !entry.HaircutFactor.IsPositive() {
		return math.LegacyZeroDec(), nil
	}

	haircut, err := decimal.Mul(math.LegacyNewDecFromInt(gross), entry.HaircutFactor)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("haircutting %s of %s: %w", gross, entry.Denom, err)
	}
	credit, err := rates.Convert(sdk.DecCoin{Denom: entry.Denom, Amount: haircut}, chain.NoahBaseDenom)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("crediting %s of %s: %w", gross, entry.Denom, err)
	}
	return credit.Amount, nil
}

// RecognitionStake is one asset's input to SolveRecognition: its cap ratio
// and its un-clipped credit.
type RecognitionStake struct {
	Ratio math.LegacyDec
	Raw   math.LegacyDec
}

// RecognitionResult is one asset's share of the solved certificate: the anoah
// ceiling its ratio resolved to, and the credit after clipping against it.
type RecognitionResult struct {
	Ceiling math.Int
	Credit  math.Int
}

// SolveRecognition resolves every cap ratio against the recognised total it
// is a share of, returning one result per stake in input order. The total T
// satisfies T = base + Σ min(raw, ratio × T); sorting by the clip threshold
// raw ÷ ratio makes the clipped set a suffix, and the one consistent split
// gives T = (base + Σ unclipped raw) ÷ (1 − Σ clipped ratios) in closed form.
// Every step is checked LegacyDec arithmetic, so T is bounded by
// base ÷ (1 − Σratios) and an unrepresentable operand is an error rather than
// a saturated figure.
func SolveRecognition(base math.Int, stakes []RecognitionStake) ([]RecognitionResult, error) {
	results := make([]RecognitionResult, len(stakes))
	for i := range results {
		results[i] = RecognitionResult{Ceiling: math.ZeroInt(), Credit: math.ZeroInt()}
	}
	baseValue := math.LegacyZeroDec()
	if !base.IsNil() && base.IsPositive() {
		baseValue = math.LegacyNewDecFromInt(base)
	}

	// Only a positive ratio holds a share of the certificate, and only a
	// positive raw credit occupies any of it.
	solve := make([]int, 0, len(stakes))
	allRatios := math.LegacyZeroDec()
	for i, stake := range stakes {
		if stake.Ratio.IsNil() || !stake.Ratio.IsPositive() {
			continue
		}
		var err error
		if allRatios, err = decimal.Add(allRatios, stake.Ratio); err != nil {
			return nil, fmt.Errorf("summing recognition cap ratios: %w", err)
		}
		if !stake.Raw.IsNil() && stake.Raw.IsPositive() {
			solve = append(solve, i)
		}
	}
	// Unreachable under a validated policy, whose ratios sum strictly below
	// one; answered conservatively rather than trusted, because past one the
	// fixed point stops existing and zero credit is the only bounded answer.
	if allRatios.GTE(math.LegacyOneDec()) {
		return results, nil
	}

	// Ascending clip thresholds raw ÷ ratio, computed up front because checked
	// division has an error to report and a sort comparator has nowhere to
	// report it. The index tiebreak keeps equal thresholds deterministic.
	thresholds := make(map[int]math.LegacyDec, len(solve))
	for _, index := range solve {
		threshold, err := decimal.Quo(stakes[index].Raw, stakes[index].Ratio)
		if err != nil {
			return nil, fmt.Errorf("sizing the clip threshold for stake %d: %w", index, err)
		}
		thresholds[index] = threshold
	}
	sort.Slice(solve, func(a, b int) bool {
		left, right := thresholds[solve[a]], thresholds[solve[b]]
		if !left.Equal(right) {
			return left.LT(right)
		}
		return solve[a] < solve[b]
	})

	// Prefix sums of raw credit and suffix sums of ratio: candidate split k
	// keeps the first k assets unclipped and clips the rest.
	unclipped := make([]math.LegacyDec, len(solve)+1)
	unclipped[0] = math.LegacyZeroDec()
	for k, index := range solve {
		sum, err := decimal.Add(unclipped[k], stakes[index].Raw)
		if err != nil {
			return nil, fmt.Errorf("summing unclipped recognition credit: %w", err)
		}
		unclipped[k+1] = sum
	}
	clippedRatio := make([]math.LegacyDec, len(solve)+1)
	clippedRatio[len(solve)] = math.LegacyZeroDec()
	for k := len(solve) - 1; k >= 0; k-- {
		sum, err := decimal.Add(clippedRatio[k+1], stakes[solve[k]].Ratio)
		if err != nil {
			return nil, fmt.Errorf("summing clipped recognition cap ratios: %w", err)
		}
		clippedRatio[k] = sum
	}

	// breaches reports whether a stake's raw credit exceeds its share of the
	// total a candidate split implies, compared cross-multiplied so neither
	// side divides.
	breaches := func(index int, denominator, numerator math.LegacyDec) (bool, error) {
		held, err := decimal.Mul(stakes[index].Raw, denominator)
		if err != nil {
			return false, fmt.Errorf("sizing stake %d against the split: %w", index, err)
		}
		permitted, err := decimal.Mul(stakes[index].Ratio, numerator)
		if err != nil {
			return false, fmt.Errorf("sizing stake %d's share of the split: %w", index, err)
		}
		return held.GT(permitted), nil
	}

	// The scan advances while the first still-clipped stake fits inside its
	// share of the candidate split and breaks at the first that does not. The
	// unclipped side must NOT be re-checked: advancing past a stake already
	// certifies its fit, and a re-fired comparison at a sub-quantum rounding
	// tie would skip the break and run the scan to the permissive
	// all-unclipped end — the one split the base bound does not survive. At
	// k = len(solve) the break is unconditional, so the loop is total.
	denominator, numerator := math.LegacyOneDec(), baseValue
	for k := 0; k <= len(solve); k++ {
		var err error
		if denominator, err = decimal.Sub(math.LegacyOneDec(), clippedRatio[k]); err != nil {
			return nil, fmt.Errorf("sizing the recognition denominator: %w", err)
		}
		if numerator, err = decimal.Add(baseValue, unclipped[k]); err != nil {
			return nil, fmt.Errorf("sizing the recognition numerator: %w", err)
		}
		if k < len(solve) {
			over, err := breaches(solve[k], denominator, numerator)
			if err != nil {
				return nil, err
			}
			if !over {
				continue
			}
		}
		break
	}

	// The denominator is positive: the clipped ratios are at most the full
	// ratio sum, already held strictly below one above.
	total, err := decimal.Quo(numerator, denominator)
	if err != nil {
		return nil, fmt.Errorf("solving recognised capital: %w", err)
	}

	// Ceilings and credits derive uniformly from the one solved T, and both
	// truncate to whole anoah on the way out.
	for i, stake := range stakes {
		if stake.Ratio.IsNil() || !stake.Ratio.IsPositive() {
			continue
		}
		ceiling, err := decimal.Mul(stake.Ratio, total)
		if err != nil {
			return nil, fmt.Errorf("sizing the recognition ceiling for stake %d: %w", i, err)
		}
		results[i].Ceiling = ceiling.TruncateInt()
		if stake.Raw.IsNil() || !stake.Raw.IsPositive() {
			continue
		}
		if stake.Raw.LT(ceiling) {
			results[i].Credit = stake.Raw.TruncateInt()
		} else {
			results[i].Credit = results[i].Ceiling
		}
	}
	return results, nil
}
