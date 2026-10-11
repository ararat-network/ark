package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

// claimSubmission carries one already-authorized claim request into the shared submission core.
// Mandate is the appointment the committee handler checked for window and term, nil for governance,
// so its presence is the claim's origin.
type claimSubmission struct {
	Submitter string
	Mandate   *types.ClaimsMandate
	Reference string
	Recipient string
	Amount    sdk.Coin
	MaxOffer  math.Int
}

// submitClaim funds and reserves a claim without paying it: a stablecoin claim converts what custody
// lacks here, so settlement only sends. Both origins use Claims params; a committee claim also closes
// inside its mandate and charges the term's gross limit with what it drew.
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
	now := chain.BlockSeconds(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting Claims params: %w", err)
	}
	// Block time fits the header's int64 and the period is capped at a year, so this cannot wrap.
	closingTime := now + params.ClaimCancellationPeriod
	origin, term := types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE, uint64(0)
	if sub.Mandate != nil {
		origin, term = types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE, sub.Mandate.Term
		if closingTime > sub.Mandate.ExpiryTime {
			return 0, fmt.Errorf(
				"claim closing time %d exceeds Claims mandate expiry time %d",
				closingTime,
				sub.Mandate.ExpiryTime,
			)
		}
	}
	claimID, err := k.NextClaimID.Peek(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting next claim ID: %w", err)
	}
	claim := types.Claim{
		ClaimId:         claimID,
		Submitter:       sub.Submitter,
		Origin:          origin,
		MandateTerm:     term,
		Reference:       sub.Reference,
		Recipient:       sub.Recipient,
		Amount:          sub.Amount,
		Status:          types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight: chain.BlockHeight(ctx),
		SubmittedTime:   now,
		ClosingTime:     closingTime,
		Charge:          math.ZeroInt(),
	}
	// The asset lookup and the valuation below need a well-formed amount before Validate runs.
	if err := types.ValidateClaimAmount(claim.Amount); err != nil {
		return 0, err
	}
	if err := k.validateDenom(ctx, claim.Amount.Denom); err != nil {
		return 0, err
	}
	recipient, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
	if err != nil {
		return 0, err
	}
	if k.bankKeeper.BlockedAddr(recipient) {
		return 0, fmt.Errorf("claim recipient %s is blocked from receiving funds", claim.Recipient)
	}
	// Every check runs before a stablecoin claim buys what custody lacks, so a refused claim never
	// trades. A committee claim charges at least its face value, which stands in for the charge until
	// the buy is known and refuses a claim past the limit, or one the oracle cannot value, before it
	// reaches Market.
	var value valuation
	if sub.Mandate != nil {
		if value, err = k.valuation(ctx, claim.Amount.Denom); err != nil {
			return 0, err
		}
		if claim.Charge, err = value.charge(claim.Amount, math.ZeroInt()); err != nil {
			return 0, err
		}
		if _, err := k.usageAfter(ctx, claim.Charge, sub.Mandate.CommitteeClaimLimit); err != nil {
			return 0, err
		}
	}
	if err := claim.Validate(); err != nil {
		return 0, err
	}
	funded, err := k.fund(ctx, claim.Amount, sub.MaxOffer)
	if err != nil {
		return 0, err
	}
	if sub.Mandate != nil {
		// Only a buy moves the charge past face value.
		if c := funded.conversion; c != nil {
			if claim.Charge, err = value.charge(sdk.NewCoin(claim.Amount.Denom, funded.custody), c.offer); err != nil {
				return 0, err
			}
		}
		used, err := k.usageAfter(ctx, claim.Charge, sub.Mandate.CommitteeClaimLimit)
		if err != nil {
			return 0, err
		}
		if err := k.ClaimsAllowanceUsed.Set(ctx, used); err != nil {
			return 0, fmt.Errorf("setting Claims allowance usage: %w", err)
		}
	}
	if err := k.reserve(ctx, claim.Amount); err != nil {
		return 0, err
	}
	if err := k.NextClaimID.Set(ctx, claimID+1); err != nil {
		return 0, fmt.Errorf("setting next claim ID: %w", err)
	}
	if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return 0, fmt.Errorf("setting claim %d: %w", claim.ClaimId, err)
	}
	if err := k.DueClaims.Set(ctx, collections.Join(claim.ClosingTime, claim.ClaimId)); err != nil {
		return 0, fmt.Errorf("indexing claim %d for settlement: %w", claim.ClaimId, err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventClaimSubmitted{
		ClaimId: claim.ClaimId,
	}); err != nil {
		return 0, fmt.Errorf("emitting claim submission event: %w", err)
	}
	if c := funded.conversion; c != nil {
		if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventClaimConverted{
			ClaimId:     claim.ClaimId,
			AmountDenom: c.output.Denom,
			Amount:      c.output.Amount,
			Offer:       c.offer,
		}); err != nil {
			return 0, fmt.Errorf("emitting claim conversion event: %w", err)
		}
	}
	return claim.ClaimId, nil
}

// usageAfter is the term's usage once charge is added, refused past the limit. Term allowance is
// gross: every submission consumes it permanently. Submission and genesis enforce its limit; only
// mandate replacement resets usage.
func (k *Keeper) usageAfter(ctx context.Context, charge, limit math.Int) (math.Int, error) {
	used, err := k.ClaimsAllowanceUsed.Get(ctx)
	if err != nil {
		return math.Int{}, fmt.Errorf("getting Claims allowance usage: %w", err)
	}
	used, err = used.SafeAdd(charge)
	if err != nil {
		return math.Int{}, fmt.Errorf("adding Claims allowance usage: %w", err)
	}
	if used.GT(limit) {
		return math.Int{}, fmt.Errorf("claim charge %s takes Claims allowance usage to %s, past mandate limit %s", charge, used, limit)
	}
	return used, nil
}

// cancelClaim releases the Insurance reservation of one pending claim before
// its closing time and records which authority vetoed it. Both handlers have
// already authenticated their signer; what reaches here is the domain that
// signed, which is the whole of what the cancellation decides.
func (k *Keeper) cancelClaim(ctx context.Context, canceller types.ClaimAuthority, claimID uint64) error {
	claim, err := k.getClaim(ctx, claimID)
	if err != nil {
		return err
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
	now := chain.BlockSeconds(ctx)
	scheduled := claim.ClosingTime
	if now >= scheduled {
		return fmt.Errorf(
			"claim %d cancellation period ended at time %d",
			claim.ClaimId,
			claim.ClosingTime,
		)
	}

	claim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	claim.ClosingTime = now
	claim.CancelledBy = canceller
	if err := claim.Validate(); err != nil {
		return fmt.Errorf("invalid cancelled claim: %w", err)
	}
	// A converted claim's stablecoin stays in custody, uncommitted, for the next claim in it.
	if err := k.release(ctx, claim.Amount); err != nil {
		return err
	}
	if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
		return fmt.Errorf("cancelling claim %d: %w", claim.ClaimId, err)
	}
	// A cancelled claim never comes due. Its record stays; only the queue entry
	// goes.
	if err := k.DueClaims.Remove(ctx, collections.Join(scheduled, claim.ClaimId)); err != nil {
		return fmt.Errorf("clearing settlement index for claim %d: %w", claim.ClaimId, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventClaimCancelled{
		ClaimId:     claim.ClaimId,
		CancelledBy: canceller,
	}); err != nil {
		return fmt.Errorf("emitting claim cancellation event: %w", err)
	}
	return nil
}

// settleClaim pays or terminally fails a due pending claim after read-only payability checks.
// Record and recipient checks use current rules. Store and event errors propagate; unpayable claims,
// a payment Bank refuses included, release their reservations. See x/claims/README.md.
func (k *Keeper) settleClaim(ctx context.Context, claim types.Claim) error {
	// fail logs why the claim cannot be paid and ends it; the reason stays in node logs, not the event.
	fail := func(reason string, details ...any) error {
		k.Logger(ctx).Error("failing unpayable claim", append([]any{"claim_id", claim.ClaimId, "reason", reason}, details...)...)
		return k.failClaim(ctx, claim)
	}
	recipient, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
	if err != nil {
		return fail("recipient can no longer be parsed", "error", err)
	}
	if k.bankKeeper.BlockedAddr(recipient) {
		return fail("recipient is blocked from receiving funds")
	}
	if balance := k.balance(ctx, claim.Amount.Denom); balance.LT(claim.Amount.Amount) {
		return fail("insurance balance is below claim amount", "balance", balance, "amount", claim.Amount.Amount)
	}

	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	if err := claim.Validate(); err != nil {
		// A record that stops passing validation is a fact about the claim, not
		// about the store: the rules moved under a stored, immutable record. It
		// is refused like any other unpayable claim rather than allowed to fail
		// the block.
		return fail("record no longer passes validation", "error", err)
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.InsuranceName,
		recipient,
		sdk.NewCoins(claim.Amount),
	); err != nil {
		// Coverage passed above, so Bank refused the recipient before moving a coin. The recipient is
		// fixed, so the claim fails like any other unpayable one.
		return fail("bank refused the payment", "error", err)
	}
	if err := k.release(ctx, claim.Amount); err != nil {
		return err
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

// failClaim marks an unpayable claim terminal and releases its reservation without retry. It must
// not revalidate the record: invalid stored records are one reason settlement calls this fallback.
func (k *Keeper) failClaim(ctx context.Context, claim types.Claim) error {
	claim.Status = types.ClaimStatus_CLAIM_STATUS_FAILED
	if err := k.release(ctx, claim.Amount); err != nil {
		return err
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

// getClaim reads a claim, reporting absence as the module's not-found.
func (k *Keeper) getClaim(ctx context.Context, id uint64) (types.Claim, error) {
	claim, err := k.Claims.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Claim{}, errorsmod.Wrapf(types.ErrClaimNotFound, "%d", id)
		}
		return types.Claim{}, fmt.Errorf("getting claim %d: %w", id, err)
	}
	return claim, nil
}
