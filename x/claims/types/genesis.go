package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

// NewGenesisState creates a Claims genesis state.
func NewGenesisState(
	params Params,
	claimsMandate ClaimsMandate,
	claimsAllowanceUsed math.Int,
	insuranceReserved math.Int,
	nextClaimID uint64,
	claims []Claim,
) *GenesisState {
	return &GenesisState{
		Params:              params,
		ClaimsMandate:       claimsMandate,
		ClaimsAllowanceUsed: claimsAllowanceUsed,
		InsuranceReserved:   insuranceReserved,
		NextClaimId:         nextClaimID,
		Claims:              append([]Claim(nil), claims...),
	}
}

// DefaultGenesisState returns the safe, unconfigured Claims genesis state.
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(
		DefaultParams(),
		DefaultClaimsMandate(),
		math.ZeroInt(),
		math.ZeroInt(),
		1,
		[]Claim{},
	)
}

// Validate checks the context-free Claims genesis invariants. The Insurance
// balance backing the reservation is validated by the keeper, which is the
// only layer with Bank state.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

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
	if gs.ClaimsAllowanceUsed.GT(claimsMandate.CommitteeClaimLimit.Amount) {
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

	allowanceUsed := math.ZeroInt()
	insuranceReserved := math.ZeroInt()
	for i, claim := range gs.Claims {
		// Claims are permanent, so IDs run from one without a gap and the sequence sits one past
		// them; an export that lost a claim cannot import.
		if claim.ClaimId != uint64(i)+1 {
			return fmt.Errorf("genesis claims must run from ID 1 without a gap: claim %d at index %d", claim.ClaimId, i)
		}
		if err := claim.Validate(); err != nil {
			return fmt.Errorf("invalid claim %d: %w", claim.ClaimId, err)
		}
		if claim.MandateTerm > claimsMandate.Term {
			return fmt.Errorf(
				"claim %d mandate term %d exceeds current Claims mandate term %d",
				claim.ClaimId,
				claim.MandateTerm,
				claimsMandate.Term,
			)
		}
		// A pending committee claim of the current term must close inside the
		// window that authorised it, mirroring the bound submission enforces.
		// Prior terms cannot be re-checked: their envelopes are no longer part
		// of state.
		if claim.Status == ClaimStatus_CLAIM_STATUS_PENDING &&
			claim.Origin == ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE &&
			claim.MandateTerm == claimsMandate.Term &&
			claim.ClosingHeight > claimsMandate.ExpiryHeight {
			return fmt.Errorf(
				"claim %d closing height %d exceeds its authorising mandate expiry height %d",
				claim.ClaimId,
				claim.ClosingHeight,
				claimsMandate.ExpiryHeight,
			)
		}
		if claim.Origin == ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE &&
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
	if gs.NextClaimId != uint64(len(gs.Claims))+1 {
		return fmt.Errorf("next claim ID %d must follow the %d genesis claims", gs.NextClaimId, len(gs.Claims))
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
