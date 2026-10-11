package types

import (
	"errors"
	"fmt"
	"math/bits"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

// MaxSchedulePeriods bounds a shape's further periods, MaxPeriodLength each period, and MaxWeight
// each weight, so a shape's duration and total weight fit without a check per grant.
const (
	MaxSchedulePeriods = 1_200
	MaxPeriodLength    = uint64(100 * 365 * 86_400)
	MaxWeight          = uint64(1) << 32
)

// Validate bounds a shape: a first period, then at most MaxSchedulePeriods further periods, each
// within a century and with a positive weight of at most MaxWeight.
func (s Schedule) Validate() error {
	if s.FirstLength == 0 || s.FirstLength > MaxPeriodLength || s.FirstWeight == 0 || s.FirstWeight > MaxWeight {
		return errors.New("first period requires a bounded positive length and weight")
	}
	if s.Periods > MaxSchedulePeriods {
		return fmt.Errorf("schedule has at most %d further periods", MaxSchedulePeriods)
	}
	if s.Periods > 0 && (s.PeriodLength == 0 || s.PeriodLength > MaxPeriodLength || s.PeriodWeight == 0 || s.PeriodWeight > MaxWeight) {
		return errors.New("further periods require a bounded positive length and weight")
	}
	// Unreachable: the bounds above hold the duration to at most 1,201 centuries. Formed checked, so
	// the backstop still holds if they are raised.
	hi, further := bits.Mul64(uint64(s.Periods), s.PeriodLength)
	duration, carry := bits.Add64(s.FirstLength, further, 0)
	if hi != 0 || carry != 0 || !fitsInt64(duration) {
		return errors.New("schedule duration overflows time")
	}
	return nil
}

// Duration is the shape's length in seconds; the bounds keep it to at most 1,201 centuries.
func (s Schedule) Duration() uint64 {
	return s.FirstLength + uint64(s.Periods)*s.PeriodLength
}

// TotalWeight is what shares divide by; the bounds keep it under 2^43.
func (s Schedule) TotalWeight() uint64 {
	return s.FirstWeight + uint64(s.Periods)*s.PeriodWeight
}

// Shares splits amount over the shape: the first share and each further share floor, the last
// period takes the remainder, and every period must receive a positive base-unit amount. With no
// further periods the first period is the last and takes the whole amount.
func (s Schedule) Shares(amount math.Int) (first, each, last math.Int, err error) {
	if err := chain.ValidateAmount(amount, true); err != nil {
		return math.Int{}, math.Int{}, math.Int{}, err
	}
	if err := s.Validate(); err != nil {
		return math.Int{}, math.Int{}, math.Int{}, err
	}
	if s.Periods == 0 {
		return amount, math.ZeroInt(), amount, nil
	}
	total := math.NewIntFromUint64(s.TotalWeight())
	first, err = mulDiv(amount, math.NewIntFromUint64(s.FirstWeight), total)
	if err != nil {
		return math.Int{}, math.Int{}, math.Int{}, err
	}
	each, err = mulDiv(amount, math.NewIntFromUint64(s.PeriodWeight), total)
	if err != nil {
		return math.Int{}, math.Int{}, math.Int{}, err
	}
	// Floors sum below the amount, so the remainder is at least each.
	last = amount.Sub(first).Sub(each.MulRaw(int64(s.Periods - 1)))
	if first.IsZero() || each.IsZero() || !last.IsPositive() {
		return math.Int{}, math.Int{}, math.Int{}, errors.New("every period must receive a positive amount")
	}
	return first, each, last, nil
}

// mulDiv floors a payment ratio after a checked multiplication.
func mulDiv(amount, numerator, denominator math.Int) (math.Int, error) {
	if amount.IsNegative() || numerator.IsNegative() || !denominator.IsPositive() {
		return math.Int{}, errors.New("ratio requires nonnegative operands and a positive denominator")
	}
	product, err := amount.SafeMul(numerator)
	if err != nil {
		return math.Int{}, fmt.Errorf("payment ratio product: %w", err)
	}
	return product.Quo(denominator), nil
}
