package types

import (
	"errors"
	"fmt"
	stdmath "math"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

const (
	// MaxStartOffset bounds a grant's start time to a century either side of the block.
	MaxStartOffset = MaxPeriodLength
	// PaymentRetryInterval is how long a grant waits for another look after Bank refused its payment,
	// in seconds.
	PaymentRetryInterval = uint64(86_400)
)

// ValidateStartTime bounds a grant's clock origin to a century of the block either way.
func ValidateStartTime(start, now uint64) error {
	if !positiveInt64(start) || !fitsInt64(now) {
		return errors.New("start time must be a positive Unix time")
	}
	if start > now+MaxStartOffset || start+MaxStartOffset < now {
		return errors.New("start time must be within a century of the block")
	}
	return nil
}

// Validate checks permanent grant terms and conservation independently of Bank.
func (g Grant) Validate() error {
	if g.Id == 0 || g.Id == stdmath.MaxUint64 {
		return errors.New("invalid grant ID")
	}
	for _, address := range []string{g.Beneficiary, g.Payee, g.CreatedBy} {
		if _, err := chain.ParseCanonicalAccountAddress("grant address", address); err != nil {
			return err
		}
	}
	if err := g.Amount.Validate(); err != nil {
		return err
	}
	if _, _, _, err := g.Schedule.Shares(g.Amount.Amount); err != nil {
		return err
	}
	// Nothing pays before one period after an award, so every first period holds the two-week window
	// to cancel what a stolen key made (D126).
	if g.Schedule.FirstLength < MinFirstPeriod {
		return errors.New("a grant's first period must be at least two weeks")
	}
	if g.Amount.Denom != chain.NoahBaseDenom {
		if err := chain.ValidatePricedDenom(g.Amount.Denom); err != nil {
			return err
		}
	}
	if g.Kind != GrantKind_GRANT_KIND_MEMBER && g.Kind != GrantKind_GRANT_KIND_OWNERSHIP && g.Kind != GrantKind_GRANT_KIND_COMPENSATION {
		return errors.New("unknown grant kind")
	}
	if g.Kind != GrantKind_GRANT_KIND_COMPENSATION && g.Amount.Denom != chain.NoahBaseDenom {
		return errors.New("distribution grants require NOAH")
	}
	if err := chain.ValidateAmount(g.AwardValue, g.Kind == GrantKind_GRANT_KIND_COMPENSATION); err != nil {
		return fmt.Errorf("award value: %w", err)
	}
	if g.Kind != GrantKind_GRANT_KIND_COMPENSATION && !g.AwardValue.IsZero() {
		return errors.New("only compensation carries an award value")
	}
	// The committee awards compensation alone (D113), so a term on compensation is the committee's.
	committee := g.Kind == GrantKind_GRANT_KIND_COMPENSATION && g.MandateTerm != 0
	if err := chain.ValidateAmount(g.Charge, committee); err != nil {
		return fmt.Errorf("charge: %w", err)
	}
	if !committee && !g.Charge.IsZero() {
		return errors.New("only a committee award carries a charge")
	}
	if err := chain.ValidateReference("grant reference", g.Reference, true); err != nil {
		return err
	}
	for _, v := range []math.Int{g.Paid, g.Remaining} {
		if err := chain.ValidateAmount(v, false); err != nil {
			return err
		}
	}
	if !positiveInt64(g.StartTime) || !positiveInt64(g.CreatedTime) || !positiveInt64(g.CreatedHeight) {
		return errors.New("invalid grant creation time or height")
	}
	// A member's clock is its registration; an ownership clock never starts before its award (D116).
	if ValidateStartTime(g.StartTime, g.CreatedTime) != nil ||
		(g.Kind == GrantKind_GRANT_KIND_MEMBER && g.StartTime != g.CreatedTime) ||
		(g.Kind == GrantKind_GRANT_KIND_OWNERSHIP && g.StartTime < g.CreatedTime) {
		return errors.New("grant clock does not fit its kind")
	}
	if g.Kind == GrantKind_GRANT_KIND_COMPENSATION {
		if s, err := CompensationSchedule(g.Schedule.FirstLength, g.Schedule.Periods+1); err != nil || s != g.Schedule {
			return errors.New("compensation is equal periods of at least two weeks, at most ten years in all")
		}
	}
	// A cancel may fall before a clock the grantor set ahead; nothing has accrued then.
	if g.Cancelled() && (g.CutoffTime < g.CreatedTime || !fitsInt64(g.CutoffTime) || g.SuspendedTime != 0) {
		return errors.New("invalid cancellation cutoff")
	}
	// A live grant still holds its whole principal; a cancelled one holds exactly its unpaid earned debt.
	if !g.Cancelled() && !g.Paid.Add(g.Remaining).Equal(g.Amount.Amount) {
		return errors.New("grant principal does not balance")
	}
	earned, _, err := g.Accrued(stdmath.MaxInt64)
	if err != nil {
		return err
	}
	if g.Paid.GT(earned) || (g.Cancelled() && !g.Remaining.Equal(earned.Sub(g.Paid))) {
		return errors.New("grant exceeds its earned entitlement")
	}
	if g.SuspendedTime != 0 && (g.Kind != GrantKind_GRANT_KIND_MEMBER || g.SuspendedTime < g.CreatedTime || !fitsInt64(g.SuspendedTime)) {
		return errors.New("invalid member suspension")
	}
	if g.Suspended() {
		if earned, _, err := g.Accrued(g.SuspendedTime); err != nil || g.Paid.GT(earned) {
			return errors.New("suspended member paid past its suspension")
		}
	}
	// Appointed grants carry their term: every member the registrar's, compensation the committee
	// awarded the committee's; ownership is governance's alone (D113).
	if (g.Kind == GrantKind_GRANT_KIND_MEMBER && g.MandateTerm == 0) || (g.Kind == GrantKind_GRANT_KIND_OWNERSHIP && g.MandateTerm != 0) {
		return errors.New("a member carries its registrar term and ownership none")
	}
	// A grant waits for a look exactly while it owes principal and is not suspended, and never
	// before one period after its award.
	if (g.DueTime == 0) != (g.Suspended() || g.Remaining.IsZero()) || !fitsInt64(g.DueTime) || (g.DueTime != 0 && g.DueTime < g.StartTime) {
		return errors.New("invalid grant due time")
	}
	if g.DueTime != 0 && g.DueTime < g.CreatedTime+g.Schedule.FirstLength {
		return errors.New("grant is due before one period after its award")
	}
	return nil
}

// Suspended reports whether a member's payments are paused; a suspension stands until reinstated.
func (g Grant) Suspended() bool {
	return g.SuspendedTime != 0
}

// Cancelled reports whether entitlement was frozen; a cancelled grant pays only its retained debt.
func (g Grant) Cancelled() bool {
	return g.CutoffTime != 0
}

// Accrued returns earned principal and the next accrual time. Cancellation freezes the original
// clock; nothing accrues before one period after the award, whatever the kind and wherever the
// clock starts, so a backdated grant or a member inside its hold can be cancelled whole until then
// (D116, D126). Every time here is bounded, so the sums fit.
func (g Grant) Accrued(now uint64) (math.Int, uint64, error) {
	s := g.Schedule
	first, each, _, err := s.Shares(g.Amount.Amount)
	if err != nil {
		return math.Int{}, 0, err
	}
	if g.StartTime > stdmath.MaxInt64-s.Duration() {
		return math.Int{}, 0, errors.New("schedule end overflows time")
	}
	if !fitsInt64(g.CreatedTime) {
		return math.Int{}, 0, errors.New("grant creation time overflows")
	}
	if g.Cancelled() {
		now = g.CutoffTime
	}
	earned, next := math.ZeroInt(), uint64(0)
	firstEnd := g.StartTime + s.FirstLength
	switch {
	case now < firstEnd:
		next = firstEnd
	case now >= g.StartTime+s.Duration():
		earned = g.Amount.Amount // The shares sum to the amount.
	default:
		// Periods is positive here, and elapsed below it, a uint32.
		elapsed := (now - firstEnd) / s.PeriodLength
		earned = first.Add(each.Mul(math.NewIntFromUint64(elapsed)))
		next = firstEnd + (elapsed+1)*s.PeriodLength
	}
	if gate := g.CreatedTime + s.FirstLength; now < gate {
		earned = math.ZeroInt()
		// What the clock has earned by the gate accrues at it.
		if gate >= firstEnd {
			next = gate
		}
	}
	if g.Cancelled() {
		next = 0
	}
	return earned, next, nil
}

// RetryAt is when the module looks again at a grant whose payment Bank refused: its next accrual
// boundary or one retry interval from now, whichever is sooner.
func RetryAt(now, next uint64) uint64 {
	retry := now + PaymentRetryInterval // Block time is below MaxInt64; the sum fits.
	if next != 0 && next < retry {
		return next
	}
	return retry
}
