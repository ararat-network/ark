package types

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ararat-network/ark/pkg/chain"
)

const (
	ModuleName              = "disbursement"
	StoreKey                = ModuleName
	DefaultMaxMembers       = uint64(1_000)
	DefaultWindowSeconds    = uint64(604_800)
	DefaultMemberAmount     = int64(10_000)
	DefaultOwnershipCeiling = int64(60_000_000)
)

// DefaultMemberSchedule pays a tenth immediately and the rest in twelve periods.
func DefaultMemberSchedule() []Period {
	schedule := []Period{{Length: 1, Parts: 4}}
	for range 12 {
		schedule = append(schedule, Period{Length: 2_628_000, Parts: 3})
	}
	return schedule
}

// DefaultParams starts with registration disabled and NOAH compensation admitted.
func DefaultParams() Params {
	return Params{
		MaxMembers: DefaultMaxMembers, WindowSeconds: DefaultWindowSeconds,
		MemberAmount: chain.NativeBaseAmount(DefaultMemberAmount), MemberSchedule: DefaultMemberSchedule(),
		CompensationDenoms: []string{chain.NoahBaseDenom},
	}
}

// DefaultOwnershipPolicy is the disbursement plan's fifth-of-others and 60M ceiling.
func DefaultOwnershipPolicy() OwnershipPolicy {
	return OwnershipPolicy{Numerator: 1, Denominator: 5, Ceiling: chain.NativeBaseAmount(DefaultOwnershipCeiling)}
}

// Validate bounds all operational work at the governance write.
func (p Params) Validate() error {
	if p.Registrar != "" {
		if _, err := chain.ParseCanonicalAccountAddress("registrar", p.Registrar); err != nil {
			return err
		}
	}
	if p.MaxMembers == 0 || p.MaxMembers > MaxIssuanceMembers {
		return fmt.Errorf("max_members must be 1..%d", MaxIssuanceMembers)
	}
	if p.WindowSeconds == 0 || p.WindowSeconds > MaxPeriodLength {
		return errors.New("issuance window must be positive and at most a century")
	}
	if _, err := Split(p.MemberAmount, p.MemberSchedule); err != nil {
		return err
	}
	if len(p.CompensationDenoms) > MaxCompensationDenoms || !slices.IsSorted(p.CompensationDenoms) {
		return errors.New("compensation denominations must be sorted and bounded")
	}
	for i, denom := range p.CompensationDenoms {
		if denom != chain.NoahBaseDenom {
			if err := chain.ValidatePricedDenom(denom); err != nil {
				return err
			}
		}
		if i > 0 && denom == p.CompensationDenoms[i-1] {
			return errors.New("duplicate compensation denomination")
		}
	}
	return nil
}

// Validate checks immutable ownership policy bounds.
func (p OwnershipPolicy) Validate() error {
	if p.Numerator == 0 || p.Numerator >= p.Denominator || p.Denominator > 1_000_000 {
		return errors.New("ownership ratio must be between zero and one with denominator at most 1000000")
	}
	return ValidateAmount(p.Ceiling, true)
}
