package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

func (m msgServer) SetClaimsMandate(ctx context.Context, msg *types.MsgSetClaimsMandate) (*types.MsgSetClaimsMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set claims mandate message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	current, err := m.k.ClaimsMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims mandate: %w", err)
	}
	if current.Term == ^uint64(0) {
		return nil, errors.New("Claims mandate term cannot advance")
	}
	term := current.Term + 1
	claimsMandate := types.NewDisabledClaimsMandate(term)
	if msg.Committee != "" {
		claimsMandate = types.ClaimsMandate{
			Term:                     term,
			Committee:                msg.Committee,
			ActivationHeight:         msg.ActivationHeight,
			ExpiryHeight:             msg.ExpiryHeight,
			CancellationPeriodBlocks: msg.CancellationPeriodBlocks,
			CommitteeClaimLimit:      msg.CommitteeClaimLimit,
		}
		if err := claimsMandate.Validate(); err != nil {
			return nil, err
		}
	}
	if claimsMandate.Committee == m.k.authority || claimsMandate.Committee == msg.Authority {
		return nil, errors.New("Claims committee must be distinct from Treasury authority")
	}
	monetaryMandate, err := m.k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary mandate: %w", err)
	}
	if monetaryMandate.Committee != "" && claimsMandate.Committee == monetaryMandate.Committee {
		return nil, errors.New("Claims committee must be distinct from the monetary-policy committee")
	}
	if err := m.k.ClaimsMandate.Set(ctx, claimsMandate); err != nil {
		return nil, fmt.Errorf("setting Claims mandate: %w", err)
	}
	if err := m.k.ClaimsAllowanceUsed.Set(ctx, math.ZeroInt()); err != nil {
		return nil, fmt.Errorf("resetting Claims allowance usage: %w", err)
	}
	return &types.MsgSetClaimsMandateResponse{}, nil
}

func (m msgServer) SubmitClaim(ctx context.Context, msg *types.MsgSubmitClaim) (*types.MsgSubmitClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil submit claim message")
	}
	mandate, err := m.k.ClaimsMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims mandate: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if sdkCtx.BlockHeight() < 0 {
		return nil, errors.New("claim submission requires a nonnegative block height")
	}
	height := uint64(sdkCtx.BlockHeight())
	if !mandate.IsActive(height) {
		return nil, errors.New("Claims mandate is not active")
	}
	if msg.ExpectedTerm != mandate.Term {
		return nil, fmt.Errorf("Claims mandate term mismatch: expected %d, got %d", mandate.Term, msg.ExpectedTerm)
	}
	if mandate.CancellationPeriodBlocks > ^uint64(0)-height {
		return nil, errors.New("claim executable height overflows")
	}
	executableHeight := height + mandate.CancellationPeriodBlocks
	if executableHeight > mandate.ExpiryHeight {
		return nil, fmt.Errorf(
			"claim executable height %d exceeds Claims mandate expiry height %d",
			executableHeight,
			mandate.ExpiryHeight,
		)
	}
	claimID, err := m.k.NextClaimID.Peek(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting next claim ID: %w", err)
	}
	if claimID == 0 {
		return nil, errors.New("next claim ID must be positive")
	}
	if claimID == ^uint64(0) {
		return nil, errors.New("claim ID sequence is exhausted")
	}
	origin := types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE
	if sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Submitter) == nil {
		origin = types.ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE
	}
	claim := types.Claim{
		ClaimId:           claimID,
		Submitter:         msg.Submitter,
		Origin:            origin,
		MandateTerm:       mandate.Term,
		IncidentReference: msg.IncidentReference,
		Recipient:         msg.Recipient,
		Amount:            msg.Amount,
		EvidenceReference: msg.EvidenceReference,
		Status:            types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight:   height,
		ExecutableHeight:  executableHeight,
	}
	if err := claim.Validate(); err != nil {
		return nil, err
	}
	recipient, err := types.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
	if err != nil {
		return nil, err
	}
	if m.k.bankKeeper.BlockedAddr(recipient) {
		return nil, fmt.Errorf("claim recipient %s is blocked from receiving funds", claim.Recipient)
	}
	if claim.Submitter != mandate.Committee && sdk.ValidateAuthority(sdkCtx, m.k.authority, claim.Submitter) != nil {
		return nil, errors.New("claim submitter is neither Treasury authority nor the exact Claims committee")
	}
	amount := claim.Amount.Amount
	allowanceUsed := math.ZeroInt()
	if claim.Origin == types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE {
		currentAllowanceUsed, err := m.k.ClaimsAllowanceUsed.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting Claims allowance usage: %w", err)
		}
		remaining, err := mandate.CommitteeClaimLimit.SafeSub(currentAllowanceUsed)
		if err != nil || remaining.IsNegative() {
			return nil, fmt.Errorf(
				"Claims allowance usage %s exceeds mandate limit %s",
				currentAllowanceUsed,
				mandate.CommitteeClaimLimit,
			)
		}
		if amount.GT(remaining) {
			return nil, fmt.Errorf(
				"claim amount %s exceeds remaining Claims allowance %s",
				amount,
				remaining,
			)
		}
		allowanceUsed, err = currentAllowanceUsed.SafeAdd(amount)
		if err != nil {
			return nil, fmt.Errorf("adding Claims allowance usage: %w", err)
		}
	}
	insuranceReserved, err := m.k.InsuranceReserved.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Insurance reservation: %w", err)
	}
	nextInsuranceReserved, err := insuranceReserved.SafeAdd(amount)
	if err != nil {
		return nil, fmt.Errorf("adding Insurance reservation: %w", err)
	}
	balance := m.k.bankKeeper.GetBalance(
		ctx,
		m.k.accountKeeper.GetModuleAddress(types.InsuranceName),
		chain.MicroNoahDenom,
	)
	if balance.Amount.LT(nextInsuranceReserved) {
		return nil, fmt.Errorf("insurance balance %s cannot cover Insurance reservation %s", balance.Amount, nextInsuranceReserved)
	}
	if claim.Origin == types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE {
		if err := m.k.ClaimsAllowanceUsed.Set(ctx, allowanceUsed); err != nil {
			return nil, fmt.Errorf("setting Claims allowance usage: %w", err)
		}
	}
	if err := m.k.InsuranceReserved.Set(ctx, nextInsuranceReserved); err != nil {
		return nil, fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := m.k.NextClaimID.Set(ctx, claimID+1); err != nil {
		return nil, fmt.Errorf("setting next claim ID: %w", err)
	}
	if err := m.k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return nil, fmt.Errorf("setting claim %d: %w", claim.ClaimId, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventClaimSubmitted{
		ClaimId: claim.ClaimId,
	}); err != nil {
		return nil, fmt.Errorf("emitting Treasury claim submission event: %w", err)
	}
	return &types.MsgSubmitClaimResponse{ClaimId: claim.ClaimId}, nil
}

// CancelClaim releases one Insurance reservation during its cancellation period.
// Governance may cancel any claim. The current committee may cancel only a
// claim that was not submitted by governance.
func (m msgServer) CancelClaim(ctx context.Context, msg *types.MsgCancelClaim) (*types.MsgCancelClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil cancel claim message")
	}
	if msg.ClaimId == 0 {
		return nil, errors.New("claim ID must be positive")
	}
	if _, err := types.ParseCanonicalAccountAddress("signer", msg.Signer); err != nil {
		return nil, err
	}
	if err := types.ValidateClaimReference("reason", msg.Reason, true); err != nil {
		return nil, err
	}
	if err := types.ValidateClaimReference("reference", msg.Reference, false); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	isGovernance := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Signer) == nil
	if !isGovernance {
		if sdkCtx.BlockHeight() < 0 {
			return nil, errors.New("claim cancellation requires a nonnegative block height")
		}
		mandate, err := m.k.ClaimsMandate.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting Claims mandate: %w", err)
		}
		if !mandate.IsActive(uint64(sdkCtx.BlockHeight())) || msg.Signer != mandate.Committee {
			return nil, errors.New("signer is neither Treasury authority nor the exact Claims committee")
		}
		if msg.ExpectedTerm != mandate.Term {
			return nil, fmt.Errorf("Claims mandate term mismatch: expected %d, got %d", mandate.Term, msg.ExpectedTerm)
		}
	}

	claim, err := m.k.Claims.Get(ctx, msg.ClaimId)
	if err != nil {
		return nil, fmt.Errorf("getting claim %d: %w", msg.ClaimId, err)
	}
	if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
		return nil, fmt.Errorf("claim %d is not pending", claim.ClaimId)
	}
	if !isGovernance && claim.Origin == types.ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE {
		return nil, fmt.Errorf("Claims committee cannot cancel governance-submitted claim %d", claim.ClaimId)
	}
	if sdkCtx.BlockHeight() < 0 {
		return nil, errors.New("claim cancellation requires a nonnegative block height")
	}
	height := uint64(sdkCtx.BlockHeight())
	if height >= claim.ExecutableHeight {
		return nil, fmt.Errorf(
			"claim %d cancellation period ended at height %d",
			claim.ClaimId,
			claim.ExecutableHeight,
		)
	}

	insuranceReserved, err := m.k.InsuranceReserved.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Insurance reservation: %w", err)
	}
	amount := claim.Amount.Amount
	nextInsuranceReserved := insuranceReserved.Sub(amount)
	claim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	claim.FinalizedHeight = height
	claim.FinalizedBy = msg.Signer
	claim.CancellationReason = msg.Reason
	claim.CancellationReference = msg.Reference
	if err := claim.Validate(); err != nil {
		return nil, fmt.Errorf("invalid cancelled claim: %w", err)
	}
	if err := m.k.InsuranceReserved.Set(ctx, nextInsuranceReserved); err != nil {
		return nil, fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := m.k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return nil, fmt.Errorf("cancelling claim %d: %w", claim.ClaimId, err)
	}
	return &types.MsgCancelClaimResponse{}, nil
}

// ExecuteClaim pays the immutable stored claim after its cancellation period
// has elapsed. Any account may execute it.
func (m msgServer) ExecuteClaim(ctx context.Context, msg *types.MsgExecuteClaim) (*types.MsgExecuteClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil execute claim message")
	}
	if msg.ClaimId == 0 {
		return nil, errors.New("claim ID must be positive")
	}
	if _, err := types.ParseCanonicalAccountAddress("caller", msg.Caller); err != nil {
		return nil, err
	}
	claim, err := m.k.Claims.Get(ctx, msg.ClaimId)
	if err != nil {
		return nil, fmt.Errorf("getting claim %d: %w", msg.ClaimId, err)
	}
	if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
		return nil, fmt.Errorf("claim %d is not pending", claim.ClaimId)
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if height < claim.ExecutableHeight {
		return nil, fmt.Errorf(
			"claim %d is not executable before height %d",
			claim.ClaimId,
			claim.ExecutableHeight,
		)
	}

	insuranceReserved, err := m.k.InsuranceReserved.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Insurance reservation: %w", err)
	}
	amount := claim.Amount.Amount
	nextInsuranceReserved := insuranceReserved.Sub(amount)
	balanceBefore := m.k.bankKeeper.GetBalance(
		ctx,
		m.k.accountKeeper.GetModuleAddress(types.InsuranceName),
		chain.MicroNoahDenom,
	)
	if balanceBefore.Amount.LT(amount) {
		return nil, fmt.Errorf("insurance balance %s is below claim amount %s", balanceBefore.Amount, amount)
	}
	recipient, err := types.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
	if err != nil {
		return nil, err
	}
	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	claim.FinalizedHeight = height
	claim.FinalizedBy = msg.Caller
	if err := claim.Validate(); err != nil {
		return nil, fmt.Errorf("invalid finalised claim: %w", err)
	}
	if err := m.k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.InsuranceName,
		recipient,
		sdk.NewCoins(claim.Amount),
	); err != nil {
		return nil, fmt.Errorf("paying claim %d: %w", claim.ClaimId, err)
	}
	if err := m.k.InsuranceReserved.Set(ctx, nextInsuranceReserved); err != nil {
		return nil, fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := m.k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return nil, fmt.Errorf("finalising claim %d: %w", claim.ClaimId, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventClaimPaid{
		ClaimId:     claim.ClaimId,
		Recipient:   claim.Recipient,
		AmountDenom: claim.Amount.Denom,
		Amount:      claim.Amount.Amount,
	}); err != nil {
		return nil, fmt.Errorf("emitting Treasury claim payment event: %w", err)
	}
	return &types.MsgExecuteClaimResponse{}, nil
}
