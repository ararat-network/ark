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
	if count == 0 || uint64(count) > MaxSchedulePeriods+1 {
		return Schedule{}, fmt.Errorf("a compensation award has 1..%d periods", MaxSchedulePeriods+1)
	}
	// count and period are bounded before the product, so it fits.
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
	if duration <= window || window == 0 {
		return value, nil
	}
	product, err := value.SafeMul(math.NewIntFromUint64(window))
	if err != nil {
		return math.Int{}, fmt.Errorf("compensation charge: %w", err)
	}
	d := math.NewIntFromUint64(duration)
	return product.Add(d).SubRaw(1).Quo(d), nil
}
