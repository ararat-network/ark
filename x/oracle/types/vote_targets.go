package types

import (
	"fmt"
	"slices"

	chain "ark/pkg/chain"
)

// VoteTargetSet is the target epoch selected for one vote-extension height.
type VoteTargetSet struct {
	Version uint64
	Denoms  []string
}

// NewVoteTargets returns the initial canonical vote-target state derived from
// params.
func NewVoteTargets(params Params) VoteTargets {
	return VoteTargets{
		Denoms:  VoteTargetDenoms(params),
		Version: InitialVoteTargetVersion,
	}
}

// VoteTargetDenoms returns the sorted target denoms configured by params.
func VoteTargetDenoms(params Params) []string {
	denoms := make([]string, len(params.TobinTaxes))
	for i, tobinTax := range params.TobinTaxes {
		denoms[i] = tobinTax.Denom
	}
	slices.Sort(denoms)
	return denoms
}

// AtHeight returns the target epoch validators must report for voteHeight.
func (v VoteTargets) AtHeight(voteHeight int64) VoteTargetSet {
	if v.Pending != nil && voteHeight >= v.Pending.ActivationVoteHeight {
		return VoteTargetSet{
			Version: v.Pending.Version,
			Denoms:  slices.Clone(v.Pending.Denoms),
		}
	}
	return VoteTargetSet{Version: v.Version, Denoms: slices.Clone(v.Denoms)}
}

// Validate checks active and pending target-state invariants.
func (v VoteTargets) Validate() error {
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
	if len(denoms) > MaxVoteTargets {
		return fmt.Errorf(
			"%s count %d exceeds maximum vote targets %d",
			label,
			len(denoms),
			MaxVoteTargets,
		)
	}
	if !slices.IsSorted(denoms) {
		return fmt.Errorf("%s must be sorted", label)
	}
	for i, denom := range denoms {
		if err := chain.ValidateMicroDenom(denom); err != nil {
			return fmt.Errorf("%s %w", label, err)
		}
		if i > 0 && denom == denoms[i-1] {
			return fmt.Errorf("%s contains duplicate denom %s", label, denom)
		}
	}
	return nil
}
