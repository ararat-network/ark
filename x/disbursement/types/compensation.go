package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

const (
	// MinFirstPeriod floors every grant's first period at two weeks: nothing pays before one period
	// after an award, so the period is the window to cancel what a stolen committee or registrar key
	// made (D126).
	MinFirstPeriod = uint64(14 * 86_400)
	// MaxCompensationDuration bounds a compensation award, periods × period, at ten years of 26
	// fortnights whatever the cadence (D116, D130).
	MaxCompensationDuration = uint64(260 * 14 * 86_400)
	// MaxCompensationBackdate bounds how far before its award a compensation clock may start: a
	// quarter, 13 weeks (D122, D130).
	MaxCompensationBackdate = uint64(13 * 7 * 86_400)
)

// CompensationSchedule is a count of equal periods of at least two weeks, one for a bounty, together
// at most ten years (D116, D126).
func CompensationSchedule(period uint64, count uint32) (Schedule, error) {
	if count == 0 {
		return Schedule{}, errors.New("a compensation award has at least one period")
	}
	// count is a uint32 and period is bounded first, so the product fits; the two-week floor then
	// holds count to 260, inside MaxSchedulePeriods.
	if period < MinFirstPeriod || period > MaxCompensationDuration || uint64(count)*period > MaxCompensationDuration {
		return Schedule{}, errors.New("a compensation award runs periods of at least two weeks, at most ten years in all")
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
