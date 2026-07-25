package types

import (
	"fmt"
	"slices"

	chain "ark/pkg/chain"
)

// OracleTargetSet is the target epoch selected for one vote-extension height.
type OracleTargetSet struct {
	Version uint64
	Denoms  []string
}

// NewOracleTargets returns initial canonical target state for denoms.
func NewOracleTargets(denoms []string) OracleTargets {
	denoms = slices.Clone(denoms)
	slices.Sort(denoms)
	return OracleTargets{
		Denoms:  denoms,
		Version: InitialOracleTargetVersion,
	}
}

// AtHeight returns the target epoch validators must report for voteHeight.
func (v OracleTargets) AtHeight(voteHeight int64) OracleTargetSet {
	if v.Pending != nil && voteHeight >= v.Pending.ActivationVoteHeight {
		return OracleTargetSet{
			Version: v.Pending.Version,
			Denoms:  slices.Clone(v.Pending.Denoms),
		}
	}

	return OracleTargetSet{
		Version: v.Version,
		Denoms:  slices.Clone(v.Denoms),
	}
}

// Validate checks active and pending target-state invariants.
func (v OracleTargets) Validate() error {
	if v.Version == 0 {
		return fmt.Errorf("active vote-target version must be positive")
	}
	if err := validateVoteTargetDenoms("active vote targets", v.Denoms); err != nil {
		return err
	}
	if v.Pending == nil {
		return nil
	}
	if v.Pending.Version != v.Version+1 {
		return fmt.Errorf(
			"pending vote-target version %d must follow active version %d",
			v.Pending.Version,
			v.Version,
		)
	}
	if v.Pending.ActivationVoteHeight <= 0 {
		return fmt.Errorf(
			"pending vote-target activation height must be positive: %d",
			v.Pending.ActivationVoteHeight,
		)
	}
	if slices.Equal(v.Pending.Denoms, v.Denoms) {
		return fmt.Errorf("pending vote targets must differ from active vote targets")
	}

	return validateVoteTargetDenoms("pending vote targets", v.Pending.Denoms)
}

func validateVoteTargetDenoms(label string, denoms []string) error {
	if len(denoms) > MaxOracleTargets {
		return fmt.Errorf(
			"%s count %d exceeds maximum vote targets %d",
			label,
			len(denoms),
			MaxOracleTargets,
		)
	}
	if !slices.IsSorted(denoms) {
		return fmt.Errorf("%s must be sorted", label)
	}
	for i, denom := range denoms {
		if err := chain.ValidateNativeBaseDenom(denom); err != nil {
			return fmt.Errorf("%s %w", label, err)
		}
		if denom == chain.NoahBaseDenom {
			return fmt.Errorf("%s must not contain native denom %s", label, denom)
		}
		if i > 0 && denom == denoms[i-1] {
			return fmt.Errorf("%s contains duplicate denom %s", label, denom)
		}
	}

	return nil
}
