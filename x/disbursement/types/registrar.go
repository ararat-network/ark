package types

import (
	"fmt"

	"github.com/ararat-network/ark/pkg/mandate"
)

// RegistrarMandateLabel names the registrar appointment in its errors.
const RegistrarMandateLabel = "registrar mandate"

// DefaultRegistrarMandate returns the canonical disabled registrar.
func DefaultRegistrarMandate() RegistrarMandate {
	return NewDisabledRegistrarMandate(0)
}

// NewDisabledRegistrarMandate returns a canonical disabled registrar at the supplied term.
func NewDisabledRegistrarMandate(term uint64) RegistrarMandate {
	return RegistrarMandate{Envelope: mandate.Disabled(term)}
}

// Validate checks either the canonical disabled registrar or one complete appointment. The open
// member tranche is the registrar's only bound, so the appointment is the envelope alone.
func (m RegistrarMandate) Validate() error {
	if err := m.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", RegistrarMandateLabel, err)
	}
	return nil
}

// WithinCommittee checks an appointed registrar sits under an appointed committee, inside its window.
// The registrar is the committee's delegate and never acts while the committee cannot; a disabled
// registrar sits under anything.
func (m RegistrarMandate) WithinCommittee(committee GrantsMandate) error {
	if m.IsDisabled() {
		return nil
	}
	if committee.IsDisabled() {
		return fmt.Errorf("%s: a registrar is appointed under a disabled %s", RegistrarMandateLabel, GrantsMandateLabel)
	}
	if m.ActivationHeight < committee.ActivationHeight || m.ExpiryHeight > committee.ExpiryHeight {
		return fmt.Errorf("%s: window [%d, %d) must sit inside the committee's [%d, %d)", RegistrarMandateLabel, m.ActivationHeight, m.ExpiryHeight, committee.ActivationHeight, committee.ExpiryHeight)
	}
	return nil
}
