package types

import (
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/mandate"
)

// GrantsMandateLabel names the shared appointment envelope in committee errors.
const GrantsMandateLabel = "grants mandate"

// CommitteeMsgs is the committee surface: the messages its mandate authorises, which the priority
// lane carries and vouches for.
var CommitteeMsgs = []sdk.Msg{&MsgCommitteeRegister{}, &MsgCommitteeSuspend{}, &MsgCommitteeReinstate{}, &MsgCommitteeCompensate{}}

// DefaultGrantsMandate returns the canonical disabled mandate.
func DefaultGrantsMandate() GrantsMandate { return NewDisabledGrantsMandate(0) }

// NewDisabledGrantsMandate returns a canonical disabled mandate at the supplied term.
func NewDisabledGrantsMandate(term uint64) GrantsMandate {
	return GrantsMandate{Envelope: mandate.Disabled(term)}
}

// Validate checks either the canonical disabled mandate or one complete appointment. The issuance
// window stays operational policy in Params; the compensation bounds belong to the appointment.
func (m GrantsMandate) Validate() error {
	if err := m.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", GrantsMandateLabel, err)
	}
	if m.IsDisabled() {
		if !m.CompensationAllowance.Empty() || m.MinFirstPeriod != 0 {
			return fmt.Errorf("%s: a disabled mandate carries no compensation power", GrantsMandateLabel)
		}
		return nil
	}
	if err := m.CompensationAllowance.Validate(); err != nil {
		return fmt.Errorf("%s: compensation allowance: %w", GrantsMandateLabel, err)
	}
	for _, c := range m.CompensationAllowance {
		if err := ValidateAmount(c.Amount, true); err != nil {
			return fmt.Errorf("%s: compensation allowance: %w", GrantsMandateLabel, err)
		}
	}
	if m.MinFirstPeriod > MaxPeriodLength {
		return fmt.Errorf("%s: minimum first period exceeds a century", GrantsMandateLabel)
	}
	// The delay is what lets governance cancel a stolen key's awards before anything accrues.
	if !m.CompensationAllowance.Empty() && m.MinFirstPeriod == 0 {
		return errors.New(GrantsMandateLabel + ": a compensation allowance needs a positive minimum first period")
	}
	return nil
}
