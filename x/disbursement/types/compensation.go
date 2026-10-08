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
	// MaxCompensationBackdate bounds how far before its award a compensation clock may start: a
	// quarter, three plan months (D122).
	MaxCompensationBackdate = uint64(3 * 2_628_000)
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

// CompensationCharge is what an award counts against the record's cap over span: its whole value
// when it runs within span, otherwise the value spread over its duration, rounded up as a bound
// (D119). Span is a window, and in an award's first window that window plus its backdate (D122).
func CompensationCharge(value math.Int, duration, span uint64) (math.Int, error) {
	if err := ValidateAmount(value, false); err != nil {
		return math.Int{}, fmt.Errorf("compensation charge: %w", err)
	}
	if duration <= span || span == 0 {
		return value, nil
	}
	// Unreachable: 128 bits by 64 fit in 256, so the rounding add below cannot overflow either.
	product, err := value.SafeMul(math.NewIntFromUint64(span))
	if err != nil {
		return math.Int{}, fmt.Errorf("compensation charge: %w", err)
	}
	d := math.NewIntFromUint64(duration)
	return product.Add(d).SubRaw(1).Quo(d), nil
}

// CompensationBackdate is how far before its award a clock starts; zero for one that starts at or
// after it.
func CompensationBackdate(start, awarded uint64) uint64 {
	if start >= awarded {
		return 0
	}
	return awarded - start
}
