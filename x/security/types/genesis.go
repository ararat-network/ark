package types

import (
	"fmt"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(securityMandate SecurityMandate, committeePlan CommitteePlan) *GenesisState {
	return &GenesisState{
		SecurityMandate: securityMandate,
		CommitteePlan:   committeePlan,
	}
}

// DefaultGenesisState returns the default security genesis state: no committee
// appointed and no plan recorded.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		SecurityMandate: DefaultSecurityMandate(),
		CommitteePlan:   CommitteePlan{},
	}
}

// Validate validates the provided security genesis state. The plan record is
// not required to agree with the mandate: it describes a plan in x/upgrade
// that outlives any appointment, and a chain must be able to start from its
// own export.
func (gs GenesisState) Validate() error {
	if err := gs.SecurityMandate.Validate(); err != nil {
		return err
	}
	if err := gs.CommitteePlan.Validate(); err != nil {
		return fmt.Errorf("invalid committee plan: %w", err)
	}

	return nil
}
