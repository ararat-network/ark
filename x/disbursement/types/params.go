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
	// DefaultCompensationWindow is a year of twelve plan months.
	DefaultCompensationWindow = uint64(12 * 2_628_000)
	// DefaultCompensationPeriod is a fortnight; an award counts them (D116).
	DefaultCompensationPeriod = uint64(14 * 86_400)
	// DefaultOwnershipCap bounds the ownership one record's grants commit from the pools, in whole
	// NOAH: the founding band's top and the founder's grant (D115, D121).
	DefaultOwnershipCap = int64(15_000_000)
)

// DefaultMemberSchedule pays a tenth at registration and the rest in twelve monthly periods.
func DefaultMemberSchedule() Schedule {
	return Schedule{
		FirstLength:  1,
		FirstWeight:  4,
		PeriodLength: 2_628_000,
		PeriodWeight: 3,
		Periods:      12,
	}
}

// DefaultOwnershipSchedule is a one-year cliff paying a quarter, weighted as the 26 fortnights it
// spans, then biweekly for three years (D116).
func DefaultOwnershipSchedule() Schedule {
	return Schedule{
		FirstLength:  365 * 86_400,
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
	if p.CompensationPeriod < MinCompensationPeriod || p.CompensationPeriod > MaxCompensationDuration {
		return errors.New("compensation period must be at least a week and at most ten years")
	}
	if err := ValidateAmount(p.OwnershipCap, true); err != nil {
		return fmt.Errorf("ownership cap: %w", err)
	}
	if err := p.OwnershipSchedule.Validate(); err != nil {
		return fmt.Errorf("ownership schedule: %w", err)
	}
	return nil
}
