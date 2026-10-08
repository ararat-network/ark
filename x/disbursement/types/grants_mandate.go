package types

import (
	"fmt"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/mandate"
)

// GrantsMandateLabel names the shared appointment envelope in committee errors.
const GrantsMandateLabel = "grants mandate"

// DefaultGrantsMandate returns the canonical disabled mandate.
func DefaultGrantsMandate() GrantsMandate {
	return NewDisabledGrantsMandate(0)
}

// NewDisabledGrantsMandate returns a canonical disabled mandate at the supplied term.
func NewDisabledGrantsMandate(term uint64) GrantsMandate {
	return GrantsMandate{Envelope: mandate.Disabled(term), CompensationAllowance: math.ZeroInt()}
}

// HasAllowance reports whether the term may award compensation; ownership is governance's alone (D113).
func (m GrantsMandate) HasAllowance() bool {
	return !m.CompensationAllowance.IsZero()
}

// Validate checks either the canonical disabled mandate or one complete appointment. The issuance
// window stays operational policy in Params; the award bounds belong to the appointment.
func (m GrantsMandate) Validate() error {
	if err := m.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", GrantsMandateLabel, err)
	}
	if err := ValidateAmount(m.CompensationAllowance, false); err != nil {
		return fmt.Errorf("%s: allowance: %w", GrantsMandateLabel, err)
	}
	if m.IsDisabled() && m.HasAllowance() {
		return fmt.Errorf("%s: a disabled mandate carries no award power", GrantsMandateLabel)
	}
	return nil
}
