package types

import (
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
)

// MaxClaimReferenceLength bounds a claim's required, free-form pointer to its
// off-chain case record.
const MaxClaimReferenceLength = 512

// ClaimsMandateLabel names the shared appointment envelope in Claims errors.
const ClaimsMandateLabel = "Claims mandate"

// DefaultClaimsMandate returns the canonical unconfigured sentinel.
func DefaultClaimsMandate() ClaimsMandate {
	return NewDisabledClaimsMandate(0)
}

// NewDisabledClaimsMandate returns the canonical disabled mandate at the
// supplied term.
func NewDisabledClaimsMandate(term uint64) ClaimsMandate {
	return ClaimsMandate{
		Envelope:            mandate.Disabled(term),
		CommitteeClaimLimit: chain.NoahCoin(math.ZeroInt()),
	}
}

// Validate validates either the exact unconfigured sentinel or one complete
// governed Claims mandate.
func (mandate ClaimsMandate) Validate() error {
	// ValidateNoahCoin rejects an unset or negative amount, so the checks below
	// can read the amount directly.
	if err := chain.ValidateNoahCoin("Claims committee claim limit", mandate.CommitteeClaimLimit); err != nil {
		return err
	}
	if err := mandate.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", ClaimsMandateLabel, err)
	}
	if mandate.IsDisabled() {
		if !mandate.CommitteeClaimLimit.IsZero() {
			return errors.New("unconfigured Claims mandate must be empty")
		}
		return nil
	}
	if !mandate.CommitteeClaimLimit.IsPositive() {
		return errors.New("Claims committee claim limit must be positive")
	}

	return nil
}

// Validate validates the immutable shape of one claim record.
func (claim Claim) Validate() error {
	if claim.ClaimId == 0 {
		return errors.New("claim ID must be positive")
	}
	if _, err := chain.ParseCanonicalAccountAddress("claim submitter", claim.Submitter); err != nil {
		return err
	}
	switch claim.Origin {
	case ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE:
		if claim.MandateTerm == 0 {
			return errors.New("committee claim must record its authorising mandate term")
		}
	case ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE:
		if claim.MandateTerm != 0 {
			return errors.New("governance claim must not record a mandate term")
		}
	default:
		return errors.New("claim origin is invalid")
	}
	if strings.TrimSpace(claim.Reference) == "" {
		return errors.New("claim reference must not be empty")
	}
	if len(claim.Reference) > MaxClaimReferenceLength {
		return fmt.Errorf("claim reference must not exceed %d bytes", MaxClaimReferenceLength)
	}
	if _, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient); err != nil {
		return err
	}
	if claim.Recipient == authtypes.NewModuleAddress(InsuranceName).String() {
		return errors.New("claim recipient cannot be the Insurance module account")
	}
	if err := chain.ValidateNoahCoin("claim amount", claim.Amount); err != nil {
		return err
	}
	if !claim.Amount.IsPositive() {
		return errors.New("claim amount must be positive")
	}
	// Pending and settled records retain a closing height strictly after submission. Cancelled
	// records use the veto height, which may equal submission height.
	if claim.Status == ClaimStatus_CLAIM_STATUS_CANCELLED {
		if claim.ClosingHeight < claim.SubmittedHeight {
			return errors.New("cancelled claim closing height cannot precede its submission height")
		}
	} else if claim.ClosingHeight <= claim.SubmittedHeight {
		return errors.New("claim closing height must follow its submission height")
	}
	switch claim.Status {
	case ClaimStatus_CLAIM_STATUS_PENDING:
		if claim.CancelledBy != ClaimAuthority_CLAIM_AUTHORITY_UNSPECIFIED {
			return errors.New("pending claim cannot record a canceller")
		}
	case ClaimStatus_CLAIM_STATUS_PAID, ClaimStatus_CLAIM_STATUS_FAILED:
		// The chain settles a claim itself, so neither outcome names a signer.
		if claim.CancelledBy != ClaimAuthority_CLAIM_AUTHORITY_UNSPECIFIED {
			return errors.New("settled claim cannot record a canceller")
		}
	case ClaimStatus_CLAIM_STATUS_CANCELLED:
		if claim.CancelledBy == ClaimAuthority_CLAIM_AUTHORITY_UNSPECIFIED {
			return errors.New("cancelled claim must record the authority that vetoed it")
		}
	default:
		return errors.New("claim status is invalid")
	}
	return nil
}
