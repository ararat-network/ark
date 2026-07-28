package types

import (
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	"ark/pkg/mandate"
)

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
		CommitteeClaimLimit: math.ZeroInt(),
	}
}

func (gs GenesisState) validateClaims() error {
	claimsMandate := gs.ClaimsMandate
	if err := claimsMandate.Validate(); err != nil {
		return err
	}
	if gs.ClaimsAllowanceUsed.IsNil() {
		return errors.New("Claims allowance usage must be set")
	}
	if gs.ClaimsAllowanceUsed.IsNegative() {
		return errors.New("Claims allowance usage must be zero or positive")
	}
	if gs.ClaimsAllowanceUsed.GT(claimsMandate.CommitteeClaimLimit) {
		return fmt.Errorf(
			"Claims allowance usage %s exceeds mandate limit %s",
			gs.ClaimsAllowanceUsed,
			claimsMandate.CommitteeClaimLimit,
		)
	}
	if gs.InsuranceReserved.IsNil() {
		return errors.New("insurance reservation must be set")
	}
	if gs.InsuranceReserved.IsNegative() {
		return errors.New("insurance reservation must be zero or positive")
	}

	if gs.NextClaimId == 0 {
		return errors.New("next claim ID must be positive")
	}
	allowanceUsed := math.ZeroInt()
	insuranceReserved := math.ZeroInt()
	for i, claim := range gs.Claims {
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %d: %w", claim.ClaimId, err)
		}
		if claim.ClaimId >= gs.NextClaimId {
			return fmt.Errorf("claim ID %d must be below next claim ID %d", claim.ClaimId, gs.NextClaimId)
		}
		if claim.MandateTerm > gs.ClaimsMandate.Term {
			return fmt.Errorf(
				"claim %d mandate term %d exceeds current Claims mandate term %d",
				claim.ClaimId,
				claim.MandateTerm,
				gs.ClaimsMandate.Term,
			)
		}
		if i > 0 && claim.ClaimId <= gs.Claims[i-1].ClaimId {
			return errors.New("genesis claims must be sorted by unique claim ID")
		}

		if claim.Origin == ClaimOrigin_CLAIM_ORIGIN_COMMITTEE &&
			claim.MandateTerm == claimsMandate.Term {
			var err error
			allowanceUsed, err = allowanceUsed.SafeAdd(claim.Amount.Amount)
			if err != nil {
				return fmt.Errorf("summing current-term committee claims: %w", err)
			}
		}
		if claim.Status == ClaimStatus_CLAIM_STATUS_PENDING {
			var err error
			insuranceReserved, err = insuranceReserved.SafeAdd(claim.Amount.Amount)
			if err != nil {
				return fmt.Errorf("summing pending claims: %w", err)
			}
		}
	}
	if !allowanceUsed.Equal(gs.ClaimsAllowanceUsed) {
		return fmt.Errorf(
			"Claims allowance usage %s does not equal current-term committee claim sum %s",
			gs.ClaimsAllowanceUsed,
			allowanceUsed,
		)
	}
	if !insuranceReserved.Equal(gs.InsuranceReserved) {
		return fmt.Errorf(
			"insurance reservation %s does not equal pending claim sum %s",
			gs.InsuranceReserved,
			insuranceReserved,
		)
	}

	return nil
}

// Validate validates either the exact unconfigured sentinel or one complete
// governed Claims mandate.
func (mandate ClaimsMandate) Validate() error {
	if mandate.CommitteeClaimLimit.IsNil() {
		return errors.New("Claims committee claim limit must be set")
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
	case ClaimOrigin_CLAIM_ORIGIN_COMMITTEE:
		if claim.MandateTerm == 0 {
			return errors.New("committee claim must record its authorizing mandate term")
		}
	case ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE:
		if claim.MandateTerm != 0 {
			return errors.New("governance claim must not record a mandate term")
		}
	default:
		return errors.New("claim origin is invalid")
	}
	if err := ValidateClaimReference("incident reference", claim.IncidentReference, true); err != nil {
		return err
	}
	if _, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient); err != nil {
		return err
	}
	if claim.Recipient == authtypes.NewModuleAddress(InsuranceName).String() {
		return errors.New("claim recipient cannot be the Insurance module account")
	}
	if !claim.Amount.IsValid() || !claim.Amount.IsPositive() || claim.Amount.Denom != chain.NoahBaseDenom {
		return fmt.Errorf("claim amount must be a valid, positive %s coin", chain.NoahBaseDenom)
	}
	if err := ValidateClaimReference("evidence reference", claim.EvidenceReference, true); err != nil {
		return err
	}
	if claim.ExecutableHeight <= claim.SubmittedHeight {
		return errors.New("claim executable height must follow its submission height")
	}
	switch claim.Status {
	case ClaimStatus_CLAIM_STATUS_PENDING:
		if claim.FinalizedHeight != 0 || claim.FinalizedBy != "" || claim.CancellationReason != "" || claim.CancellationReference != "" {
			return errors.New("pending claim cannot contain finalisation fields")
		}
	case ClaimStatus_CLAIM_STATUS_PAID:
		if claim.FinalizedHeight < claim.ExecutableHeight {
			return errors.New("paid claim cannot finalise before its executable height")
		}
		if _, err := chain.ParseCanonicalAccountAddress("claim finalised by", claim.FinalizedBy); err != nil {
			return err
		}
		if claim.CancellationReason != "" || claim.CancellationReference != "" {
			return errors.New("paid claim cannot contain cancellation fields")
		}
	case ClaimStatus_CLAIM_STATUS_CANCELLED:
		if claim.FinalizedHeight < claim.SubmittedHeight {
			return errors.New("cancelled claim cannot finalise before submission")
		}
		if claim.FinalizedHeight >= claim.ExecutableHeight {
			return errors.New("cancelled claim must finalise during its cancellation period")
		}
		if _, err := chain.ParseCanonicalAccountAddress("claim finalised by", claim.FinalizedBy); err != nil {
			return err
		}
		if err := ValidateClaimReference("cancellation reason", claim.CancellationReason, true); err != nil {
			return err
		}
		if err := ValidateClaimReference("cancellation reference", claim.CancellationReference, false); err != nil {
			return err
		}
	default:
		return errors.New("claim status is invalid")
	}
	return nil
}

// ValidateClaimReference validates a bounded free-form Claims field.
func ValidateClaimReference(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("claim %s must not be empty", field)
	}
	if len(value) > MaxClaimReferenceLength {
		return fmt.Errorf("claim %s must not exceed %d bytes", field, MaxClaimReferenceLength)
	}
	return nil
}
