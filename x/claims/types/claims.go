package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
)

// ClaimsMandateLabel names the shared appointment envelope in Claims errors.
const ClaimsMandateLabel = "Claims mandate"

// MaxSettlementsPerBlock bounds EndBlock work; claims past it stay due for the next block.
const MaxSettlementsPerBlock = 1_000

// DefaultClaimsMandate returns the canonical unconfigured sentinel.
func DefaultClaimsMandate() ClaimsMandate {
	return NewDisabledClaimsMandate(0)
}

// NewDisabledClaimsMandate returns the canonical disabled mandate at the
// supplied term.
func NewDisabledClaimsMandate(term uint64) ClaimsMandate {
	return ClaimsMandate{
		Envelope:            mandate.Disabled(term),
		CommitteeClaimLimit: math.ZeroInt(),
	}
}

// Validate validates either the exact unconfigured sentinel or one complete
// governed Claims mandate.
func (mandate ClaimsMandate) Validate() error {
	// ValidateAmount rejects an unset or negative limit, so the checks below can read it directly.
	if err := chain.ValidateAmount(mandate.CommitteeClaimLimit, false); err != nil {
		return fmt.Errorf("Claims committee claim limit: %w", err)
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

// ValidateClaimAmount checks a claim pays a positive amount of NOAH or an Ark-native asset; whether
// the asset is registered and active is the keeper's to check.
func ValidateClaimAmount(amount sdk.Coin) error {
	if err := amount.Validate(); err != nil {
		return fmt.Errorf("invalid claim amount: %w", err)
	}
	if !amount.IsPositive() {
		return errors.New("claim amount must be positive")
	}
	if amount.Denom != chain.NoahBaseDenom {
		if err := chain.ValidatePricedDenom(amount.Denom); err != nil {
			return fmt.Errorf("claim amount: %w", err)
		}
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
		if err := chain.ValidateAmount(claim.Charge, true); err != nil {
			return fmt.Errorf("committee claim charge: %w", err)
		}
	case ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE:
		if claim.MandateTerm != 0 {
			return errors.New("governance claim must not record a mandate term")
		}
		if claim.Charge.IsNil() || !claim.Charge.IsZero() {
			return errors.New("governance claim charges nothing")
		}
	default:
		return errors.New("claim origin is invalid")
	}
	if err := chain.ValidateReference("claim reference", claim.Reference, true); err != nil {
		return err
	}
	if _, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient); err != nil {
		return err
	}
	if claim.Recipient == authtypes.NewModuleAddress(InsuranceName).String() {
		return errors.New("claim recipient cannot be the Insurance module account")
	}
	if err := ValidateClaimAmount(claim.Amount); err != nil {
		return err
	}
	// Pending and settled records retain a closing time strictly after submission. Cancelled
	// records use the veto time, which may equal submission time.
	if claim.Status == ClaimStatus_CLAIM_STATUS_CANCELLED {
		if claim.ClosingTime < claim.SubmittedTime {
			return errors.New("cancelled claim closing time cannot precede its submission time")
		}
	} else if claim.ClosingTime <= claim.SubmittedTime {
		return errors.New("claim closing time must follow its submission time")
	}
	// A closing past the header's int64 is one no block reaches.
	if claim.ClosingTime > chain.MaxBlockSeconds {
		return fmt.Errorf("claim closing time %d exceeds the header's int64", claim.ClosingTime)
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
