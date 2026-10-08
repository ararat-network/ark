package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

const (
	// MinCompensationPeriod floors the compensation period at a week: nothing pays before one period
	// after an award, so the period is the window governance has to cancel a stolen key's awards.
	MinCompensationPeriod = uint64(7 * 86_400)
	// MaxCompensationDuration bounds a compensation award, periods × period, at ten years whatever
	// the cadence (D116).
	MaxCompensationDuration = uint64(10 * 365 * 86_400)
)

// CompensationSchedule is a count of equal periods of at least a week, one for a bounty, together
// at most ten years (D116).
func CompensationSchedule(period uint64, count uint32) (Schedule, error) {
	if count == 0 {
		return Schedule{}, errors.New("a compensation award has at least one period")
	}
	// count is a uint32 and period is bounded first, so the product fits; the week floor then holds
	// count to 521, inside MaxSchedulePeriods.
	if period < MinCompensationPeriod || period > MaxCompensationDuration || uint64(count)*period > MaxCompensationDuration {
		return Schedule{}, errors.New("a compensation award runs periods of at least a week, at most ten years in all")
	}
	s := Schedule{FirstLength: period, FirstWeight: 1, PeriodLength: period, PeriodWeight: 1, Periods: count - 1}
	if err := s.Validate(); err != nil {
		return Schedule{}, err
	}
	return s, nil
}

// CompensationCharge is what an award counts against the record's cap for each year it runs: its
// whole value within a window, or the value spread over its duration, rounded up as a bound (D119).
func CompensationCharge(value math.Int, duration, window uint64) (math.Int, error) {
	if err := ValidateAmount(value, false); err != nil {
		return math.Int{}, fmt.Errorf("compensation charge: %w", err)
	}
	if duration <= window || window == 0 {
		return value, nil
	}
	// Unreachable: 128 bits by 64 fit in 256, so the rounding add below cannot overflow either.
	product, err := value.SafeMul(math.NewIntFromUint64(window))
	if err != nil {
		return math.Int{}, fmt.Errorf("compensation charge: %w", err)
	}
	d := math.NewIntFromUint64(duration)
	return product.Add(d).SubRaw(1).Quo(d), nil
}
