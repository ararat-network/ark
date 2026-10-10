package types

import (
	"errors"
	"fmt"

	"github.com/ararat-network/ark/pkg/chain"
)

const (
	// DefaultMemberAmount is the plan's step 1 grant, in whole NOAH (D91).
	DefaultMemberAmount = int64(20_000)
	// DefaultCompensationCap bounds one record's compensation awards per window, in the reference
	// unit: about eight of the plan's skilled years.
	DefaultCompensationCap = int64(1_000_000)
	// DefaultCompensationWindow is a year of 26 fortnights, the pay cadence, so the longest stream, 260
	// fortnights, charges a tenth of its value a window (D129).
	DefaultCompensationWindow = uint64(26 * 14 * 86_400)
	// DefaultCompensationPeriod is a fortnight; an award counts them (D116).
	DefaultCompensationPeriod = uint64(14 * 86_400)
	// DefaultOwnershipCap bounds the ownership one record's grants commit from the pools, in whole
	// NOAH: the founder's grant (D115, D121).
	DefaultOwnershipCap = int64(15_000_000)
)

// DefaultMemberSchedule holds a member's tenth for the two-week floor, the window to suspend what a
// stolen registrar key registered, then pays the rest in thirteen four-week periods (D126, D130).
func DefaultMemberSchedule() Schedule {
	return Schedule{
		FirstLength:  MinFirstPeriod,
		FirstWeight:  13,
		PeriodLength: 28 * 86_400,
		PeriodWeight: 9,
		Periods:      13,
	}
}

// DefaultOwnershipSchedule is a cliff of 26 fortnights paying a quarter, then biweekly for three years:
// the cliff weighs 26 and each of the 78 fortnights after it one, 104 in all (D116, D130).
func DefaultOwnershipSchedule() Schedule {
	return Schedule{
		FirstLength:  26 * 14 * 86_400,
		FirstWeight:  26,
		PeriodLength: 14 * 86_400,
		PeriodWeight: 1,
		Periods:      78,
	}
}

// DefaultParams sets the member terms, the compensation terms, and the ownership terms; the
// committee is appointed by its mandate.
func DefaultParams() Params {
	return Params{
		MemberAmount:       chain.NativeBaseAmount(DefaultMemberAmount),
		MemberSchedule:     DefaultMemberSchedule(),
		CompensationCap:    chain.NativeBaseAmount(DefaultCompensationCap),
		CompensationWindow: DefaultCompensationWindow,
		CompensationPeriod: DefaultCompensationPeriod,
		OwnershipCap:       chain.NativeBaseAmount(DefaultOwnershipCap),
		OwnershipSchedule:  DefaultOwnershipSchedule(),
	}
}

// Validate bounds the member, compensation, and ownership terms at the governance write.
func (p Params) Validate() error {
	if err := p.MemberSchedule.Validate(); err != nil {
		return fmt.Errorf("member schedule: %w", err)
	}
	if _, _, _, err := p.MemberSchedule.Shares(p.MemberAmount); err != nil {
		return fmt.Errorf("member schedule: %w", err)
	}
	if err := ValidateAmount(p.CompensationCap, true); err != nil {
		return fmt.Errorf("compensation cap: %w", err)
	}
	if p.CompensationWindow == 0 || p.CompensationWindow > MaxPeriodLength {
		return errors.New("compensation window must be positive and at most a century")
	}
	if p.CompensationPeriod < MinFirstPeriod || p.CompensationPeriod > MaxCompensationDuration {
		return errors.New("compensation period must be at least two weeks and at most ten years")
	}
	if p.MemberSchedule.FirstLength < MinFirstPeriod || p.OwnershipSchedule.FirstLength < MinFirstPeriod {
		return errors.New("member and ownership first periods must be at least two weeks")
	}
	if err := ValidateAmount(p.OwnershipCap, true); err != nil {
		return fmt.Errorf("ownership cap: %w", err)
	}
	if err := p.OwnershipSchedule.Validate(); err != nil {
		return fmt.Errorf("ownership schedule: %w", err)
	}
	return nil
}
