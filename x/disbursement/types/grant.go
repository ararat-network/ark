package types

import (
	"errors"
	"fmt"
	stdmath "math"
	"strings"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

const (
	// MaxStartOffset bounds a grant's start time to a century either side of the block.
	MaxStartOffset = MaxPeriodLength
	// MaxPaymentsPerBlock bounds EndBlock work; entries past it stay due for the next block.
	MaxPaymentsPerBlock = 1_000
	// PaymentRetryInterval is how long a grant waits for another look after a cap or a refused send
	// held back part of what was due, in seconds.
	PaymentRetryInterval = uint64(86_400)
)

// ValidateStartTime bounds a grant's clock origin to a century of the block either way.
func ValidateStartTime(start, now uint64) error {
	if start == 0 || start > stdmath.MaxInt64 || now > stdmath.MaxInt64 {
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
	first, _, _, err := g.Schedule.Shares(g.Amount.Amount)
	if err != nil {
		return err
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
	if err := ValidateAmount(g.ReferenceValue, g.Kind == GrantKind_GRANT_KIND_COMPENSATION); err != nil {
		return fmt.Errorf("reference value: %w", err)
	}
	if g.Kind != GrantKind_GRANT_KIND_COMPENSATION && !g.ReferenceValue.IsZero() {
		return errors.New("only compensation carries a reference value")
	}
	if len(g.Reference) > MaxReferenceBytes || strings.TrimSpace(g.Reference) == "" {
		return errors.New("grant reference must be nonempty and bounded")
	}
	for _, v := range []math.Int{g.Paid, g.Remaining} {
		if err := ValidateAmount(v, false); err != nil {
			return err
		}
	}
	if g.StartTime == 0 || g.StartTime > stdmath.MaxInt64 || g.CreatedTime == 0 || g.CreatedTime > stdmath.MaxInt64 || g.CreatedHeight == 0 || g.CreatedHeight > stdmath.MaxInt64 {
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
			return errors.New("compensation is equal periods of at least a week, at most ten years in all")
		}
	}
	// A cancel may fall before a clock the grantor set ahead; nothing has accrued then.
	if g.Cancelled() && (g.CutoffTime < g.CreatedTime || g.CutoffTime > stdmath.MaxInt64 || g.SuspendedAt != 0) {
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
	if g.Kind == GrantKind_GRANT_KIND_MEMBER && g.Paid.LT(first) {
		return errors.New("member first period was not paid")
	}
	if g.SuspendedAt != 0 && (g.Kind != GrantKind_GRANT_KIND_MEMBER || g.SuspendedAt < g.CreatedTime || g.SuspendedAt > stdmath.MaxInt64) {
		return errors.New("invalid member suspension")
	}
	if g.Suspended() {
		if earned, _, err := g.Accrued(g.SuspendedAt); err != nil || g.Paid.GT(earned) {
			return errors.New("suspended member paid past its suspension")
		}
	}
	// Appointed grants carry their term: every member the registrar's, compensation the committee
	// awarded the committee's; ownership is governance's alone (D113).
	if (g.Kind == GrantKind_GRANT_KIND_MEMBER && g.MandateTerm == 0) || (g.Kind == GrantKind_GRANT_KIND_OWNERSHIP && g.MandateTerm != 0) {
		return errors.New("a member carries its registrar term and ownership none")
	}
	// A grant waits for a look exactly while it owes principal and is not suspended, and a
	// contributor grant never before one period after its award.
	if (g.DueAt == 0) != (g.Suspended() || g.Remaining.IsZero()) || g.DueAt > stdmath.MaxInt64 || (g.DueAt != 0 && g.DueAt < g.StartTime) {
		return errors.New("invalid grant due time")
	}
	if g.Kind != GrantKind_GRANT_KIND_MEMBER && g.DueAt != 0 && g.DueAt < g.CreatedTime+g.Schedule.FirstLength {
		return errors.New("grant is due before one period after its award")
	}
	return nil
}

// Suspended reports whether a member's payments are paused; a suspension stands until reinstated.
func (g Grant) Suspended() bool {
	return g.SuspendedAt != 0
}

// Cancelled reports whether entitlement was frozen; a cancelled grant pays only its retained debt.
func (g Grant) Cancelled() bool {
	return g.CutoffTime != 0
}

// Accrued returns earned principal and the next accrual time. Members earn their first period at
// registration; cancellation freezes the original clock; nothing accrues before one period after
// the award, wherever the clock starts, so a backdated grant can be cancelled whole until then
// (D116). Every time here is bounded, so the sums fit.
func (g Grant) Accrued(now uint64) (math.Int, uint64, error) {
	s := g.Schedule
	first, each, _, err := s.Shares(g.Amount.Amount)
	if err != nil {
		return math.Int{}, 0, err
	}
	if g.StartTime > stdmath.MaxInt64-s.Duration() {
		return math.Int{}, 0, errors.New("schedule end overflows time")
	}
	if g.CreatedTime > stdmath.MaxInt64 {
		return math.Int{}, 0, errors.New("grant creation time overflows")
	}
	if g.Cancelled() {
		now = g.CutoffTime
	}
	earned, next := math.ZeroInt(), uint64(0)
	firstEnd := g.StartTime + s.FirstLength
	switch {
	case now < firstEnd:
		// A member's first share is paid at registration, so the member's next accrual is the
		// second boundary; anyone else's is the first.
		next = firstEnd
		if g.Kind == GrantKind_GRANT_KIND_MEMBER {
			earned, next = first, 0
			if s.Periods > 0 {
				next = firstEnd + s.PeriodLength
			}
		}
	case now >= g.StartTime+s.Duration():
		earned = g.Amount.Amount // The shares sum to the amount.
	default:
		// Periods is positive here, and elapsed below it, a uint32.
		elapsed := (now - firstEnd) / s.PeriodLength
		earned = first.Add(each.MulRaw(int64(elapsed)))
		next = firstEnd + (elapsed+1)*s.PeriodLength
	}
	if g.Kind != GrantKind_GRANT_KIND_MEMBER {
		if gate := g.CreatedTime + s.FirstLength; now < gate {
			earned = math.ZeroInt()
			// What the clock has earned by the gate accrues at it.
			if gate >= firstEnd {
				next = gate
			}
		}
	}
	if g.Cancelled() {
		next = 0
	}
	return earned, next, nil
}

// NextLook is when the module next looks at a grant that still owes principal: its next accrual
// boundary, or, when a cap or a refused send held back part of what was due, the sooner of that
// boundary and one retry interval from now.
func NextLook(now, next uint64, held bool) uint64 {
	if !held && next != 0 {
		return next
	}
	retry := now + PaymentRetryInterval // Block time is below MaxInt64; the sum fits.
	if next != 0 && next < retry {
		return next
	}
	return retry
}
