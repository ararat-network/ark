package types

import (
	"errors"
	"fmt"

	"github.com/ararat-network/ark/pkg/mandate"
)

// SecurityMandateLabel names the shared appointment envelope in security
// committee errors.
const SecurityMandateLabel = "security mandate"

// DefaultSecurityMandate returns the canonical disabled committee mandate. The
// term may increase later while the mandate remains disabled.
func DefaultSecurityMandate() SecurityMandate {
	return NewDisabledSecurityMandate(0)
}

// NewDisabledSecurityMandate returns a canonical disabled mandate at the
// supplied term.
func NewDisabledSecurityMandate(term uint64) SecurityMandate {
	return SecurityMandate{Envelope: mandate.Disabled(term)}
}

// Validate validates either a disabled mandate or one complete committee
// appointment. There is nothing to check beyond the envelope, which is reached
// through the field because this method shadows the promoted one.
func (securityMandate SecurityMandate) Validate() error {
	if err := securityMandate.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", SecurityMandateLabel, err)
	}

	return nil
}

// IsZero reports whether no committee plan is recorded.
func (committeePlan CommitteePlan) IsZero() bool {
	return committeePlan.Name == "" && committeePlan.Height == 0
}

// Matches reports whether the record still describes the supplied pending
// plan. Both name and height must agree, since x/upgrade allows a plan to be
// replaced under the same name at a different height.
func (committeePlan CommitteePlan) Matches(name string, height int64) bool {
	if committeePlan.IsZero() {
		return false
	}

	return committeePlan.Name == name && committeePlan.Height == height
}

// Validate checks a recorded committee plan. The record is all-or-nothing: a
// half-populated one would make IsZero and Matches disagree.
func (committeePlan CommitteePlan) Validate() error {
	if committeePlan.IsZero() {
		if committeePlan.Term != 0 {
			return errors.New("empty committee plan must not carry a term")
		}

		return nil
	}
	if committeePlan.Name == "" {
		return errors.New("committee plan must have a name")
	}
	if committeePlan.Height <= 0 {
		return errors.New("committee plan height must be positive")
	}

	return nil
}
