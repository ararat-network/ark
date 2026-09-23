package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/mandate"
)

// RegistrarMandateLabel names the shared appointment envelope in registrar errors.
const RegistrarMandateLabel = "registrar mandate"

// CommitteeMsgs is the registrar surface: the messages its mandate authorises, which the priority
// lane carries and vouches for.
var CommitteeMsgs = []sdk.Msg{&MsgCommitteeRegister{}, &MsgCommitteeSuspend{}, &MsgCommitteeReinstate{}}

// DefaultRegistrarMandate returns the canonical disabled mandate.
func DefaultRegistrarMandate() RegistrarMandate { return NewDisabledRegistrarMandate(0) }

// NewDisabledRegistrarMandate returns a canonical disabled mandate at the supplied term.
func NewDisabledRegistrarMandate(term uint64) RegistrarMandate {
	return RegistrarMandate{Envelope: mandate.Disabled(term)}
}

// Validate checks either the canonical disabled mandate or one complete appointment. The mandate
// carries no fields of its own: the issuance window is operational policy in Params.
func (m RegistrarMandate) Validate() error {
	if err := m.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", RegistrarMandateLabel, err)
	}
	return nil
}
