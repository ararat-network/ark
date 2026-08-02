package types

import (
	"fmt"

	"ark/pkg/mandate"
)

// EmergencyMandateLabel names the shared appointment envelope in
// emergency-mandate errors.
const EmergencyMandateLabel = "emergency mandate"

// DefaultEmergencyMandate returns the canonical disabled mandate.
func DefaultEmergencyMandate() EmergencyMandate {
	return NewDisabledEmergencyMandate(0)
}

// NewDisabledEmergencyMandate returns a canonical disabled mandate at the
// supplied term.
func NewDisabledEmergencyMandate(term uint64) EmergencyMandate {
	return EmergencyMandate{Envelope: mandate.Disabled(term)}
}

// Validate checks either the canonical disabled mandate or one complete
// committee appointment. The mandate carries no domain fields of its own:
// per-term usage is separate state and reference fallbacks belong to the
// consumers that own the references.
func (m EmergencyMandate) Validate() error {
	if err := m.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", EmergencyMandateLabel, err)
	}

	return nil
}
