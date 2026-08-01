package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

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

// executeClaim pays the immutable stored claim after its cancellation period
// has elapsed and records who finalised it. The caller is already validated by
// the handler; any account may execute.
func (k *Keeper) executeClaim(ctx context.Context, caller string, claimID uint64) error {
	claim, err := k.Claims.Get(ctx, claimID)
	if err != nil {
		return fmt.Errorf("getting claim %d: %w", claimID, err)
	}
	if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
		return fmt.Errorf("claim %d is not pending", claim.ClaimId)
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if height < claim.ExecutableHeight {
		return fmt.Errorf(
			"claim %d is not executable before height %d",
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
	balanceBefore := k.bankKeeper.GetBalance(
		ctx,
		k.accountKeeper.GetModuleAddress(types.InsuranceName),
		chain.NoahBaseDenom,
	)
	if balanceBefore.Amount.LT(amount) {
		return fmt.Errorf("insurance balance %s is below claim amount %s", balanceBefore.Amount, amount)
	}
	recipient, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
	if err != nil {
		return err
	}
	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	claim.FinalizedHeight = height
	claim.FinalizedBy = caller
	if err := claim.Validate(); err != nil {
		return fmt.Errorf("invalid finalised claim: %w", err)
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.InsuranceName,
		recipient,
		sdk.NewCoins(claim.Amount),
	); err != nil {
		return fmt.Errorf("paying claim %d: %w", claim.ClaimId, err)
	}
	if err := k.InsuranceReserved.Set(ctx, nextInsuranceReserved); err != nil {
		return fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return fmt.Errorf("finalising claim %d: %w", claim.ClaimId, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventClaimPaid{
		ClaimId:     claim.ClaimId,
		Recipient:   claim.Recipient,
		AmountDenom: claim.Amount.Denom,
		Amount:      claim.Amount.Amount,
	}); err != nil {
		return fmt.Errorf("emitting Treasury claim payment event: %w", err)
	}
	return nil
}
