package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/claims/types"
)

// claimSubmission carries one already-authorized claim request into the shared
// submission core. Origin is decided by the message type that authorized it.
// The three mandate fields are committee-only facts supplied by the committee
// handler after its window and term checks; governance leaves them zero.
type claimSubmission struct {
	Submitter           string
	Origin              types.ClaimAuthority
	MandateTerm         uint64
	MandateExpiryHeight uint64
	CommitteeClaimLimit sdk.Coin
	Reference           string
	Recipient           string
	Amount              sdk.Coin
}

// submitClaim reserves one authorized claim against live Insurance coverage
// without moving coins. The cancellation period comes from Claims params for
// both origins; the core never reads the Claims mandate. A committee-origin
// submission carries its mandate facts from the handler's window and term
// checks: its closing height must not pass the mandate expiry and it
// permanently consumes the current term allowance.
func (k *Keeper) submitClaim(ctx context.Context, sub claimSubmission) (uint64, error) {
	// The claim record stores both address strings, so they are canonicalised
	// here — the single point where submissions of either origin become state —
	// and Claim.Validate keeps requiring the canonical spelling behind this.
	var err error
	if sub.Submitter, err = chain.CanonicaliseAccountAddress("claim submitter", sub.Submitter); err != nil {
		return 0, err
	}
	if sub.Recipient, err = chain.CanonicaliseAccountAddress("claim recipient", sub.Recipient); err != nil {
		return 0, err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := uint64(sdkCtx.BlockHeight())
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting Claims params: %w", err)
	}
	closingHeight := height + params.ClaimCancellationPeriodBlocks
	if sub.Origin == types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE && closingHeight > sub.MandateExpiryHeight {
		return 0, fmt.Errorf(
			"claim closing height %d exceeds Claims mandate expiry height %d",
			closingHeight,
			sub.MandateExpiryHeight,
		)
	}
	claimID, err := k.NextClaimID.Peek(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting next claim ID: %w", err)
	}
	claim := types.Claim{
		ClaimId:         claimID,
		Submitter:       sub.Submitter,
		Origin:          sub.Origin,
		MandateTerm:     sub.MandateTerm,
		Reference:       sub.Reference,
		Recipient:       sub.Recipient,
		Amount:          sub.Amount,
		Status:          types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight: height,
		ClosingHeight:   closingHeight,
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
	if claim.Origin == types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE {
		currentAllowanceUsed, err := k.ClaimsAllowanceUsed.Get(ctx)
		if err != nil {
			return 0, fmt.Errorf("getting Claims allowance usage: %w", err)
		}
		allowanceUsed, err = currentAllowanceUsed.SafeAdd(amount)
		if err != nil {
			return 0, fmt.Errorf("adding Claims allowance usage: %w", err)
		}
		// The term allowance is gross and permanent, so the test is on what the
		// term will have spent once this claim is counted, not on what is left.
		// Usage never passes the limit on its own — every prior submission came
		// through here, a replacement mandate resets it, and genesis rejects an
		// import that exceeds it — so this is the only thing standing between a
		// committee and an over-committed term.
		if allowanceUsed.GT(sub.CommitteeClaimLimit.Amount) {
			return 0, fmt.Errorf(
				"claim amount %s takes Claims allowance usage to %s, past mandate limit %s",
				amount,
				allowanceUsed,
				sub.CommitteeClaimLimit,
			)
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
	balance := k.balance(ctx)
	if balance.LT(nextInsuranceReserved) {
		return 0, fmt.Errorf("insurance balance %s cannot cover Insurance reservation %s", balance, nextInsuranceReserved)
	}
	if claim.Origin == types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE {
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
	if err := k.DueClaims.Set(ctx, collections.Join(claim.ClosingHeight, claim.ClaimId)); err != nil {
		return 0, fmt.Errorf("indexing claim %d for settlement: %w", claim.ClaimId, err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventClaimSubmitted{
		ClaimId: claim.ClaimId,
	}); err != nil {
		return 0, fmt.Errorf("emitting claim submission event: %w", err)
	}
	return claim.ClaimId, nil
}

// cancelClaim releases the Insurance reservation of one pending claim before
// its closing height and records which authority vetoed it. Both handlers have
// already authenticated their signer; what reaches here is the domain that
// signed, which is the whole of what the cancellation decides.
func (k *Keeper) cancelClaim(ctx context.Context, canceller types.ClaimAuthority, claimID uint64) error {
	claim, err := k.Claims.Get(ctx, claimID)
	if err != nil {
		return fmt.Errorf("getting claim %d: %w", claimID, err)
	}
	if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
		return fmt.Errorf("claim %d is not pending", claim.ClaimId)
	}
	// Governance may veto any claim; the committee may not overturn a
	// governance decision. With the canceller typed as a domain this reads
	// straight off the two authorities, rather than through a flag the callers
	// had to set consistently.
	if canceller == types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE &&
		claim.Origin == types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE {
		return fmt.Errorf("Claims committee cannot cancel governance-submitted claim %d", claim.ClaimId)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := uint64(sdkCtx.BlockHeight())
	scheduledHeight := claim.ClosingHeight
	if height >= scheduledHeight {
		return fmt.Errorf(
			"claim %d cancellation period ended at height %d",
			claim.ClaimId,
			claim.ClosingHeight,
		)
	}

	insuranceReserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Insurance reservation: %w", err)
	}
	amount := claim.Amount.Amount
	nextInsuranceReserved := insuranceReserved.Sub(amount)
	claim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	claim.ClosingHeight = height
	claim.CancelledBy = canceller
	if err := claim.Validate(); err != nil {
		return fmt.Errorf("invalid cancelled claim: %w", err)
	}
	if err := k.InsuranceReserved.Set(ctx, nextInsuranceReserved); err != nil {
		return fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return fmt.Errorf("cancelling claim %d: %w", claim.ClaimId, err)
	}
	// A cancelled claim never comes due. Its record stays; only the queue entry
	// goes.
	if err := k.DueClaims.Remove(ctx, collections.Join(scheduledHeight, claim.ClaimId)); err != nil {
		return fmt.Errorf("clearing settlement index for claim %d: %w", claim.ClaimId, err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventClaimCancelled{
		ClaimId:     claim.ClaimId,
		CancelledBy: canceller,
	}); err != nil {
		return fmt.Errorf("emitting claim cancellation event: %w", err)
	}
	return nil
}

// settleClaim brings one due claim to its terminal state. It pays the claim
// when Insurance can and fails it when Insurance cannot: both are settlement,
// and the sweep does not choose between them. EndBlocker is the only caller and
// has already established that the claim is pending — the sweep owns the queue,
// settlement owns the outcome.
//
// Payability is settled above the first write, and every check deciding it is a
// read, so the refusing outcome acts on state no payment attempt has touched.
// That is what keeps the two ways a payment can fail apart: a claim is failed
// for a fact about the claim, while a store that will not answer fails the
// block. Facts submission once established are re-tested rather than trusted,
// because every one of them rides with the binary rather than with state: an
// upgrade can block a recipient, stop an address parsing, or tighten the record
// rules for a claim that sat pending across it, and none of that is worth a
// halt. An error here is therefore always a store refusing to answer — never a
// verdict on the claim.
func (k *Keeper) settleClaim(ctx context.Context, claim types.Claim) error {
	recipient, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
	if err != nil {
		k.Logger(ctx).Error(
			"failing unpayable claim",
			"claim_id", claim.ClaimId,
			"reason", "recipient can no longer be parsed",
			"error", err,
		)
		return k.failClaim(ctx, claim)
	}
	if k.bankKeeper.BlockedAddr(recipient) {
		k.Logger(ctx).Error(
			"failing unpayable claim",
			"claim_id", claim.ClaimId,
			"reason", "recipient is blocked from receiving funds",
		)
		return k.failClaim(ctx, claim)
	}
	if balance := k.balance(ctx); balance.LT(claim.Amount.Amount) {
		k.Logger(ctx).Error(
			"failing unpayable claim",
			"claim_id", claim.ClaimId,
			"reason", "insurance balance is below claim amount",
			"balance", balance,
			"amount", claim.Amount.Amount,
		)
		return k.failClaim(ctx, claim)
	}

	insuranceReserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Insurance reservation: %w", err)
	}
	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	if err := claim.Validate(); err != nil {
		// A record that stops passing validation is a fact about the claim, not
		// about the store: the rules moved under a stored, immutable record. It
		// is refused like any other unpayable claim rather than allowed to fail
		// the block.
		k.Logger(ctx).Error(
			"failing unpayable claim",
			"claim_id", claim.ClaimId,
			"reason", "record no longer passes validation",
			"error", err,
		)
		return k.failClaim(ctx, claim)
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.InsuranceName,
		recipient,
		sdk.NewCoins(claim.Amount),
	); err != nil {
		return fmt.Errorf("paying claim %d: %w", claim.ClaimId, err)
	}
	if err := k.InsuranceReserved.Set(ctx, insuranceReserved.Sub(claim.Amount.Amount)); err != nil {
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
		return fmt.Errorf("emitting claim payment event: %w", err)
	}
	return nil
}

// failClaim is settlement's refusing outcome: it ends one due claim the chain
// could not pay, releasing its reservation with it. A claim that will not be
// paid must stop encumbering Insurance, which is the whole reason settlement
// stopped needing a signer.
//
// Failure is terminal on the first attempt rather than retried. A blocked or
// unparseable recipient is fixed for the life of a binary, so re-testing it at
// a later height would re-run an identical check against an identical claim.
// Coverage is the one input that could in principle recover, but submission
// proved the claim covered and settlement is the only path that spends
// Insurance, so a shortfall means the reservation accounting is wrong rather
// than the fund being briefly light — a state to investigate, not to wait out.
//
// The record is deliberately not re-validated here. failClaim is the last
// resort every unpayable claim must reach, and one of the facts that routes a
// claim here is precisely that its stored record no longer satisfies the
// current binary's rules; judging the record again would give the refusing
// outcome something to refuse, and the only place left for that refusal to go
// is a halted block. The flip itself needs no judgment: the sweep proved the
// claim pending, and failing it changes the status alone.
func (k *Keeper) failClaim(ctx context.Context, claim types.Claim) error {
	insuranceReserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting Insurance reservation: %w", err)
	}
	claim.Status = types.ClaimStatus_CLAIM_STATUS_FAILED
	if err := k.InsuranceReserved.Set(ctx, insuranceReserved.Sub(claim.Amount.Amount)); err != nil {
		return fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return fmt.Errorf("failing claim %d: %w", claim.ClaimId, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventClaimFailed{
		ClaimId: claim.ClaimId,
	}); err != nil {
		return fmt.Errorf("emitting claim failure event: %w", err)
	}
	return nil
}
