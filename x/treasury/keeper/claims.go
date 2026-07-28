package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/pkg/mandate"
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
	term, err := current.NextTerm()
	if err != nil {
		return nil, err
	}
	claimsMandate := types.NewDisabledClaimsMandate(term)
	if msg.Committee != "" {
		claimsMandate = types.ClaimsMandate{
			Envelope: mandate.Envelope{
				Term:             term,
				Committee:        msg.Committee,
				ActivationHeight: msg.ActivationHeight,
				ExpiryHeight:     msg.ExpiryHeight,
			},
			CommitteeClaimLimit: msg.CommitteeClaimLimit,
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
	if msg.Committee != "" {
		params, err := m.k.Params.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting Treasury params: %w", err)
		}
		// The envelope validated activation < expiry, so the span subtraction
		// cannot underflow. A span shorter than the cancellation period would
		// leave the committee unable to submit any claim for the whole
		// appointment, because every executable height would land past expiry.
		if msg.ExpiryHeight-msg.ActivationHeight < params.ClaimCancellationPeriodBlocks {
			return nil, errors.New("Claims mandate active span cannot be shorter than the claim cancellation period")
		}
	}
	if err := m.k.ClaimsMandate.Set(ctx, claimsMandate); err != nil {
		return nil, fmt.Errorf("setting Claims mandate: %w", err)
	}
	if err := m.k.ClaimsAllowanceUsed.Set(ctx, math.ZeroInt()); err != nil {
		return nil, fmt.Errorf("resetting Claims allowance usage: %w", err)
	}
	return &types.MsgSetClaimsMandateResponse{}, nil
}

// SubmitClaim records one governance-submitted pending claim. Governance
// submissions never consume the committee term allowance and do not depend on
// the Claims mandate; their cancellation period comes from Treasury params.
func (m msgServer) SubmitClaim(ctx context.Context, msg *types.MsgSubmitClaim) (*types.MsgSubmitClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil submit claim message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	claimID, err := m.k.submitClaim(ctx, claimSubmission{
		Submitter:         msg.Authority,
		Origin:            types.ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE,
		IncidentReference: msg.IncidentReference,
		Recipient:         msg.Recipient,
		Amount:            msg.Amount,
		EvidenceReference: msg.EvidenceReference,
	})
	if err != nil {
		return nil, err
	}
	return &types.MsgSubmitClaimResponse{ClaimId: claimID}, nil
}

// CommitteeSubmitClaim records one committee-submitted pending claim against
// the current term's gross allowance.
func (m msgServer) CommitteeSubmitClaim(ctx context.Context, msg *types.MsgCommitteeSubmitClaim) (*types.MsgCommitteeSubmitClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee submit claim message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("committee", msg.Committee); err != nil {
		return nil, err
	}
	mandate, err := m.k.ClaimsMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims mandate: %w", err)
	}
	if mandate.Committee == "" || msg.Committee != mandate.Committee {
		return nil, errors.New("signer is not the exact Claims committee")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if !mandate.IsActive(uint64(sdkCtx.BlockHeight())) {
		return nil, errors.New("Claims mandate is not active")
	}
	if err := mandate.RequireTerm(msg.ExpectedTerm); err != nil {
		return nil, err
	}
	claimID, err := m.k.submitClaim(ctx, claimSubmission{
		Submitter:           msg.Committee,
		Origin:              types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE,
		MandateTerm:         mandate.Term,
		MandateExpiryHeight: mandate.ExpiryHeight,
		CommitteeClaimLimit: mandate.CommitteeClaimLimit,
		IncidentReference:   msg.IncidentReference,
		Recipient:           msg.Recipient,
		Amount:              msg.Amount,
		EvidenceReference:   msg.EvidenceReference,
	})
	if err != nil {
		return nil, err
	}
	return &types.MsgCommitteeSubmitClaimResponse{ClaimId: claimID}, nil
}

// claimSubmission carries one already-authorized claim request into the shared
// submission core. Origin is decided by the message type that authorized it.
// The three mandate fields are committee-only facts supplied by the committee
// handler after its window and term checks; governance leaves them zero.
type claimSubmission struct {
	Submitter           string
	Origin              types.ClaimOrigin
	MandateTerm         uint64
	MandateExpiryHeight uint64
	CommitteeClaimLimit math.Int
	IncidentReference   string
	Recipient           string
	Amount              sdk.Coin
	EvidenceReference   string
}

// submitClaim reserves one authorized claim against live Insurance coverage
// without moving coins. The cancellation period comes from Treasury params for
// both origins; the core never reads the Claims mandate. A committee-origin
// submission carries its mandate facts from the handler's window and term
// checks: its executable height must not pass the mandate expiry and it
// permanently consumes the current term allowance.
func (k *Keeper) submitClaim(ctx context.Context, sub claimSubmission) (uint64, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := uint64(sdkCtx.BlockHeight())
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting Treasury params: %w", err)
	}
	executableHeight := height + params.ClaimCancellationPeriodBlocks
	if sub.Origin == types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE && executableHeight > sub.MandateExpiryHeight {
		return 0, fmt.Errorf(
			"claim executable height %d exceeds Claims mandate expiry height %d",
			executableHeight,
			sub.MandateExpiryHeight,
		)
	}
	claimID, err := k.NextClaimID.Peek(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting next claim ID: %w", err)
	}
	claim := types.Claim{
		ClaimId:           claimID,
		Submitter:         sub.Submitter,
		Origin:            sub.Origin,
		MandateTerm:       sub.MandateTerm,
		IncidentReference: sub.IncidentReference,
		Recipient:         sub.Recipient,
		Amount:            sub.Amount,
		EvidenceReference: sub.EvidenceReference,
		Status:            types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight:   height,
		ExecutableHeight:  executableHeight,
	}
	if err := claim.Validate(); err != nil {
		return 0, err
	}
	recipient, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
	if err != nil {
		return 0, err
	}
	if k.bankKeeper.BlockedAddr(recipient) {
		return 0, fmt.Errorf("claim recipient %s is blocked from receiving funds", claim.Recipient)
	}
	amount := claim.Amount.Amount
	allowanceUsed := math.ZeroInt()
	if claim.Origin == types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE {
		currentAllowanceUsed, err := k.ClaimsAllowanceUsed.Get(ctx)
		if err != nil {
			return 0, fmt.Errorf("getting Claims allowance usage: %w", err)
		}
		remaining, err := sub.CommitteeClaimLimit.SafeSub(currentAllowanceUsed)
		if err != nil || remaining.IsNegative() {
			return 0, fmt.Errorf(
				"Claims allowance usage %s exceeds mandate limit %s",
				currentAllowanceUsed,
				sub.CommitteeClaimLimit,
			)
		}
		if amount.GT(remaining) {
			return 0, fmt.Errorf(
				"claim amount %s exceeds remaining Claims allowance %s",
				amount,
				remaining,
			)
		}
		allowanceUsed, err = currentAllowanceUsed.SafeAdd(amount)
		if err != nil {
			return 0, fmt.Errorf("adding Claims allowance usage: %w", err)
		}
	}
	insuranceReserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting Insurance reservation: %w", err)
	}
	nextInsuranceReserved, err := insuranceReserved.SafeAdd(amount)
	if err != nil {
		return 0, fmt.Errorf("adding Insurance reservation: %w", err)
	}
	balance := k.bankKeeper.GetBalance(
		ctx,
		k.accountKeeper.GetModuleAddress(types.InsuranceName),
		chain.NoahBaseDenom,
	)
	if balance.Amount.LT(nextInsuranceReserved) {
		return 0, fmt.Errorf("insurance balance %s cannot cover Insurance reservation %s", balance.Amount, nextInsuranceReserved)
	}
	if claim.Origin == types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE {
		if err := k.ClaimsAllowanceUsed.Set(ctx, allowanceUsed); err != nil {
			return 0, fmt.Errorf("setting Claims allowance usage: %w", err)
		}
	}
	if err := k.InsuranceReserved.Set(ctx, nextInsuranceReserved); err != nil {
		return 0, fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := k.NextClaimID.Set(ctx, claimID+1); err != nil {
		return 0, fmt.Errorf("setting next claim ID: %w", err)
	}
	if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return 0, fmt.Errorf("setting claim %d: %w", claim.ClaimId, err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventClaimSubmitted{
		ClaimId: claim.ClaimId,
	}); err != nil {
		return 0, fmt.Errorf("emitting Treasury claim submission event: %w", err)
	}
	return claim.ClaimId, nil
}

// CancelClaim releases one Insurance reservation as the governance authority.
// Governance may cancel any pending claim regardless of origin, and its
// authorization does not depend on the current Claims mandate.
func (m msgServer) CancelClaim(ctx context.Context, msg *types.MsgCancelClaim) (*types.MsgCancelClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil cancel claim message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.cancelClaim(ctx, claimCancellation{
		Signer:                msg.Authority,
		AllowGovernanceOrigin: true,
		ClaimID:               msg.ClaimId,
		Reason:                msg.Reason,
		Reference:             msg.Reference,
	}); err != nil {
		return nil, err
	}
	return &types.MsgCancelClaimResponse{}, nil
}

// CommitteeCancelClaim releases one Insurance reservation as the exact
// appointed committee, which may cancel only a claim it could have submitted.
func (m msgServer) CommitteeCancelClaim(ctx context.Context, msg *types.MsgCommitteeCancelClaim) (*types.MsgCommitteeCancelClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee cancel claim message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("committee", msg.Committee); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	mandate, err := m.k.ClaimsMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims mandate: %w", err)
	}
	if mandate.Committee == "" || msg.Committee != mandate.Committee {
		return nil, errors.New("signer is not the exact Claims committee")
	}
	if !mandate.IsActive(uint64(sdkCtx.BlockHeight())) {
		return nil, errors.New("Claims mandate is not active")
	}
	if err := mandate.RequireTerm(msg.ExpectedTerm); err != nil {
		return nil, err
	}

	if err := m.k.cancelClaim(ctx, claimCancellation{
		Signer:                msg.Committee,
		AllowGovernanceOrigin: false,
		ClaimID:               msg.ClaimId,
		Reason:                msg.Reason,
		Reference:             msg.Reference,
	}); err != nil {
		return nil, err
	}
	return &types.MsgCommitteeCancelClaimResponse{}, nil
}

// claimCancellation carries one already-authorized cancellation into the shared
// core. Only governance may cancel a governance-submitted claim.
type claimCancellation struct {
	Signer                string
	AllowGovernanceOrigin bool
	ClaimID               uint64
	Reason                string
	Reference             string
}

// cancelClaim releases the Insurance reservation of one pending claim before
// its executable height and records who finalised it.
func (k *Keeper) cancelClaim(ctx context.Context, cancellation claimCancellation) error {
	if err := types.ValidateClaimReference("reason", cancellation.Reason, true); err != nil {
		return err
	}
	if err := types.ValidateClaimReference("reference", cancellation.Reference, false); err != nil {
		return err
	}

	claim, err := k.Claims.Get(ctx, cancellation.ClaimID)
	if err != nil {
		return fmt.Errorf("getting claim %d: %w", cancellation.ClaimID, err)
	}
	if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
		return fmt.Errorf("claim %d is not pending", claim.ClaimId)
	}
	if !cancellation.AllowGovernanceOrigin && claim.Origin == types.ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE {
		return fmt.Errorf("Claims committee cannot cancel governance-submitted claim %d", claim.ClaimId)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := uint64(sdkCtx.BlockHeight())
	if height >= claim.ExecutableHeight {
		return fmt.Errorf(
			"claim %d cancellation period ended at height %d",
			claim.ClaimId,
			claim.ExecutableHeight,
		)
	}

	insuranceReserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Insurance reservation: %w", err)
	}
	amount := claim.Amount.Amount
	nextInsuranceReserved := insuranceReserved.Sub(amount)
	claim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	claim.FinalizedHeight = height
	claim.FinalizedBy = cancellation.Signer
	claim.CancellationReason = cancellation.Reason
	claim.CancellationReference = cancellation.Reference
	if err := claim.Validate(); err != nil {
		return fmt.Errorf("invalid cancelled claim: %w", err)
	}
	if err := k.InsuranceReserved.Set(ctx, nextInsuranceReserved); err != nil {
		return fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return fmt.Errorf("cancelling claim %d: %w", claim.ClaimId, err)
	}
	return nil
}

// ExecuteClaim pays the immutable stored claim after its cancellation period
// has elapsed. Any account may execute it.
func (m msgServer) ExecuteClaim(ctx context.Context, msg *types.MsgExecuteClaim) (*types.MsgExecuteClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil execute claim message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("caller", msg.Caller); err != nil {
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
		chain.NoahBaseDenom,
	)
	if balanceBefore.Amount.LT(amount) {
		return nil, fmt.Errorf("insurance balance %s is below claim amount %s", balanceBefore.Amount, amount)
	}
	recipient, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
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
