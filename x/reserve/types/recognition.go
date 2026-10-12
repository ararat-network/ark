package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// MaxRecognitionRateAge caps per-entry freshness windows, in seconds, with substantial headroom over
// publication gaps. Recognition executes during block settlement, so valid inputs must keep its
// arithmetic bounded.
const MaxRecognitionRateAge = uint64(30 * 86_400)

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
	if entry.MaxRateAge == 0 || entry.MaxRateAge > MaxRecognitionRateAge {
		return fmt.Errorf(
			"eligibility max rate age must be greater than zero and at most %ds, is %ds",
			MaxRecognitionRateAge,
			entry.MaxRateAge,
		)
	}
	return nil
}

// GrantsCredit checks policy eligibility independently of current valuation. Valid entries have
// positive haircut and cap; a stale feed does not grant the committee permission to burn credited
// custody.
func (entry EligibilityEntry) GrantsCredit() bool {
	return entry.HaircutFactor.IsPositive() && entry.RecognitionCapRatio.IsPositive()
}

// ValidateRecognitionPolicy validates a complete eligibility set: every entry
// valid, one entry per asset, cap ratios summing to at most one. The entry
// count is deliberately unbounded — a cap would only ever bind on governance,
// the sole writer, and would size nothing.
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
	// External custody may add at most the NOAH balance itself, so recognised
	// capital stays within twice the NOAH the Reserve holds.
	if ratioSum.GT(math.LegacyOneDec()) {
		return fmt.Errorf("recognition cap ratios must sum to at most one, got %s", ratioSum)
	}
	return nil
}

// RawCredit haircuts gross base units, then values them in NOAH before the cap applies. Whole-unit
// quantity bounds and MaxExchangeRate keep the checked product representable. Missing rates fail;
// zero rates credit nothing.
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

// RecognitionStake is one asset's input to RecognitionCredits: its cap ratio
// and its uncapped credit.
type RecognitionStake struct {
	Ratio math.LegacyDec
	Raw   math.LegacyDec
}

// RecognitionResult is one asset's capped credit: the anoah ceiling its ratio
// of the NOAH balance allows, and the credit after clipping against it.
type RecognitionResult struct {
	Ceiling math.Int
	Credit  math.Int
}

// RecognitionCredits caps each stake at its ratio of the Reserve's NOAH balance and returns the
// credits in input order. Caps read only the balance, so no stake's credit moves another's.
func RecognitionCredits(base math.Int, stakes []RecognitionStake) ([]RecognitionResult, error) {
	results := make([]RecognitionResult, len(stakes))
	for i := range results {
		results[i] = RecognitionResult{Ceiling: math.ZeroInt(), Credit: math.ZeroInt()}
	}
	if base.IsNil() || !base.IsPositive() {
		return results, nil
	}

	ratios := math.LegacyZeroDec()
	for _, stake := range stakes {
		if stake.Ratio.IsNil() || !stake.Ratio.IsPositive() {
			continue
		}
		var err error
		if ratios, err = decimal.Add(ratios, stake.Ratio); err != nil {
			return nil, fmt.Errorf("summing recognition cap ratios: %w", err)
		}
	}
	// Unreachable: the policy write and genesis import hold the sum at or below one. Kept so a
	// corrupt policy answers zero credit rather than more than twice the balance.
	if ratios.GT(math.LegacyOneDec()) {
		return results, nil
	}

	balance := math.LegacyNewDecFromInt(base)
	for i, stake := range stakes {
		if stake.Ratio.IsNil() || !stake.Ratio.IsPositive() {
			continue
		}
		ceiling, err := decimal.Mul(stake.Ratio, balance)
		if err != nil {
			return nil, fmt.Errorf("sizing the recognition ceiling for stake %d: %w", i, err)
		}
		results[i].Ceiling = ceiling.TruncateInt()
		if stake.Raw.IsNil() || !stake.Raw.IsPositive() {
			continue
		}
		results[i].Credit = math.MinInt(stake.Raw.TruncateInt(), results[i].Ceiling)
	}
	return results, nil
}
