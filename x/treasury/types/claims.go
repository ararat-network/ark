package types

import (
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
)

const MaxClaimReferenceLength = 512

// DefaultClaimsMandate returns the canonical unconfigured sentinel.
func DefaultClaimsMandate() ClaimsMandate {
	return NewDisabledClaimsMandate(0)
}

// NewDisabledClaimsMandate returns the canonical disabled mandate at the
// supplied term.
func NewDisabledClaimsMandate(term uint64) ClaimsMandate {
	return ClaimsMandate{
		Term:                term,
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
	seenClaims := make(map[uint64]struct{}, len(gs.Claims))
	allowanceUsed := math.ZeroInt()
	insuranceReserved := math.ZeroInt()
	for _, claim := range gs.Claims {
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
		if _, exists := seenClaims[claim.ClaimId]; exists {
			return fmt.Errorf("duplicate claim ID %d", claim.ClaimId)
		}
		seenClaims[claim.ClaimId] = struct{}{}

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
	if mandate.Committee == "" {
		if mandate.ActivationHeight != 0 ||
			mandate.ExpiryHeight != 0 ||
			mandate.CancellationPeriodBlocks != 0 ||
			!mandate.CommitteeClaimLimit.IsZero() {
			return errors.New("unconfigured Claims mandate must be empty")
		}
		return nil
	}
	if mandate.Term == 0 {
		return errors.New("configured Claims mandate term must be positive")
	}
	if _, err := ParseCanonicalAccountAddress("Claims committee", mandate.Committee); err != nil {
		return err
	}
	if mandate.ActivationHeight >= mandate.ExpiryHeight {
		return errors.New("Claims mandate activation height must precede expiry height")
	}
	if mandate.CancellationPeriodBlocks == 0 {
		return errors.New("Claims cancellation period blocks must be positive")
	}
	if mandate.CancellationPeriodBlocks > mandate.ExpiryHeight-mandate.ActivationHeight {
		return errors.New("Claims cancellation period blocks cannot exceed the Claims mandate active span")
	}
	if !mandate.CommitteeClaimLimit.IsPositive() {
		return errors.New("Claims committee claim limit must be positive")
	}

	return nil
}

// IsActive reports whether the Claims committee appointment is active at the
// supplied height.
func (mandate ClaimsMandate) IsActive(height uint64) bool {
	return mandate.Committee != "" &&
		mandate.ActivationHeight <= height &&
		height < mandate.ExpiryHeight
}

// Validate validates the immutable shape of one claim record.
func (claim Claim) Validate() error {
	if claim.ClaimId == 0 {
		return errors.New("claim ID must be positive")
	}
	if _, err := ParseCanonicalAccountAddress("claim submitter", claim.Submitter); err != nil {
		return err
	}
	if claim.Origin != ClaimOrigin_CLAIM_ORIGIN_COMMITTEE &&
		claim.Origin != ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE {
		return errors.New("claim origin is invalid")
	}
	if claim.MandateTerm == 0 {
		return errors.New("claim mandate term must be positive")
	}
	if err := ValidateClaimReference("incident reference", claim.IncidentReference, true); err != nil {
		return err
	}
	if _, err := ParseCanonicalAccountAddress("claim recipient", claim.Recipient); err != nil {
		return err
	}
	if claim.Recipient == authtypes.NewModuleAddress(InsuranceName).String() {
		return errors.New("claim recipient cannot be the Insurance module account")
	}
	if !claim.Amount.IsValid() || !claim.Amount.IsPositive() || claim.Amount.Denom != chain.MicroNoahDenom {
		return fmt.Errorf("claim amount must be a valid, positive %s coin", chain.MicroNoahDenom)
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
		if _, err := ParseCanonicalAccountAddress("claim finalised by", claim.FinalizedBy); err != nil {
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
		if _, err := ParseCanonicalAccountAddress("claim finalised by", claim.FinalizedBy); err != nil {
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
