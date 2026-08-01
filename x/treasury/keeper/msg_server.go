package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/pkg/chain"
	"ark/pkg/mandate"
	"ark/x/treasury/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	types.UnimplementedMsgServer

	k *Keeper
}

// NewMsgServerImpl returns the Treasury message server.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// UpdateParams updates the complete governance-owned parameter set and atomically
// rebuilds tax caps when the reference cap changes.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update params message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	current, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting current params: %w", err)
	}
	policy, err := m.k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary policy: %w", err)
	}
	funding, err := m.k.RewardFunding.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting reward funding state: %w", err)
	}
	if err := types.ValidateRewardTargetCapacity(msg.Params, funding, policy); err != nil {
		return nil, err
	}

	if msg.Params.ReferenceTaxCap.Denom != current.ReferenceTaxCap.Denom {
		return nil, sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"reference tax cap denom is set by the protocol reference: re-point it with MsgSetReferenceDenom, not %s",
			msg.Params.ReferenceTaxCap.Denom,
		)
	}
	referenceChanged := !current.ReferenceTaxCap.Equal(msg.Params.ReferenceTaxCap)
	var caps []types.TaxCap
	if referenceChanged {
		denoms, err := m.k.assetKeeper.PricedLiveDenoms(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting priced-live denominations: %w", err)
		}
		caps, err = m.k.buildTaxCaps(ctx, msg.Params, denoms)
		if err != nil {
			return nil, err
		}
	}
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
	}
	if referenceChanged {
		for _, cap := range caps {
			if err := m.k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
				return nil, fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
			}
		}
		// This rebuild derived every member from current inputs, so it serves
		// an outstanding cadence refresh just as the block refresh would.
		if err := m.k.TaxCapRefreshPending.Set(ctx, false); err != nil {
			return nil, fmt.Errorf("clearing pending tax cap refresh: %w", err)
		}
		if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventTaxCapsUpdated{
			TaxCaps: caps,
		}); err != nil {
			return nil, fmt.Errorf("emitting Treasury tax-cap update event: %w", err)
		}
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// SetMonetaryMandate replaces or disables the bounded committee
// appointment. Treasury derives a new term for every replacement.
func (m msgServer) SetMonetaryMandate(ctx context.Context, msg *types.MsgSetMonetaryMandate) (*types.MsgSetMonetaryMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set monetary mandate message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	current, err := m.k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary mandate: %w", err)
	}
	envelope, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight)
	if err != nil {
		return nil, err
	}
	updated := types.NewDisabledMonetaryMandate(envelope.Term)
	updated.Envelope = envelope
	if msg.Committee != "" {
		updated.MinimumPolicy = msg.MinimumPolicy
		updated.MaximumPolicy = msg.MaximumPolicy
		if err := updated.Validate(); err != nil {
			return nil, err
		}
		if updated.Committee == m.k.authority || updated.Committee == msg.Authority {
			return nil, errors.New("monetary-policy committee must be distinct from Treasury authority")
		}
	}

	if err := m.k.MonetaryMandate.Set(ctx, updated); err != nil {
		return nil, fmt.Errorf("setting monetary mandate: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventMonetaryMandateSet{
		Term:             updated.Term,
		Committee:        updated.Committee,
		ActivationHeight: updated.ActivationHeight,
		ExpiryHeight:     updated.ExpiryHeight,
	}); err != nil {
		return nil, fmt.Errorf("emitting Treasury monetary mandate: %w", err)
	}
	return &types.MsgSetMonetaryMandateResponse{}, nil
}

// UpdatePolicy applies one complete reversible policy update as the
// governance authority. Governance overrides the committee mandate, so no term,
// window, or policy-bound check applies.
func (m msgServer) UpdatePolicy(ctx context.Context, msg *types.MsgUpdatePolicy) (*types.MsgUpdatePolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update monetary-policy message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}
	if err := m.k.applyMonetaryPolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}
	return &types.MsgUpdatePolicyResponse{}, nil
}

// CommitteeUpdatePolicy applies one complete reversible policy update
// as the exact appointed committee, during the active term and window, with
// every field inside the mandate's bounds.
func (m msgServer) CommitteeUpdatePolicy(ctx context.Context, msg *types.MsgCommitteeUpdatePolicy) (*types.MsgCommitteeUpdatePolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee update monetary-policy message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("committee", msg.Committee); err != nil {
		return nil, err
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	mandate, err := m.k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary mandate: %w", err)
	}
	if err := mandate.Authorise(msg.Committee, msg.ExpectedTerm, uint64(sdkCtx.BlockHeight())); err != nil {
		return nil, fmt.Errorf("%s: %w", types.MonetaryMandateLabel, err)
	}
	if err := mandate.ValidatePolicy(msg.Policy); err != nil {
		return nil, err
	}
	if err := m.k.applyMonetaryPolicy(ctx, msg.Policy); err != nil {
		return nil, err
	}
	return &types.MsgCommitteeUpdatePolicyResponse{}, nil
}

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
	envelope, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight)
	if err != nil {
		return nil, err
	}
	claimsMandate := types.NewDisabledClaimsMandate(envelope.Term)
	claimsMandate.Envelope = envelope
	if msg.Committee != "" {
		claimsMandate.CommitteeClaimLimit = msg.CommitteeClaimLimit
		if err := claimsMandate.Validate(); err != nil {
			return nil, err
		}
	}
	if claimsMandate.Committee == m.k.authority || claimsMandate.Committee == msg.Authority {
		return nil, errors.New("Claims committee must be distinct from Treasury authority")
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
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventClaimsMandateSet{
		Term:             claimsMandate.Term,
		Committee:        claimsMandate.Committee,
		ActivationHeight: claimsMandate.ActivationHeight,
		ExpiryHeight:     claimsMandate.ExpiryHeight,
	}); err != nil {
		return nil, fmt.Errorf("emitting Treasury Claims mandate: %w", err)
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
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := mandate.Authorise(msg.Committee, msg.ExpectedTerm, uint64(sdkCtx.BlockHeight())); err != nil {
		return nil, fmt.Errorf("%s: %w", types.ClaimsMandateLabel, err)
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
	if err := mandate.Authorise(msg.Committee, msg.ExpectedTerm, uint64(sdkCtx.BlockHeight())); err != nil {
		return nil, fmt.Errorf("%s: %w", types.ClaimsMandateLabel, err)
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

// ExecuteClaim pays the immutable stored claim after its cancellation period
// has elapsed. Any account may execute it.
func (m msgServer) ExecuteClaim(ctx context.Context, msg *types.MsgExecuteClaim) (*types.MsgExecuteClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil execute claim message")
	}
	if _, err := chain.ParseCanonicalAccountAddress("caller", msg.Caller); err != nil {
		return nil, err
	}
	if err := m.k.executeClaim(ctx, msg.Caller, msg.ClaimId); err != nil {
		return nil, err
	}
	return &types.MsgExecuteClaimResponse{}, nil
}

func (m msgServer) TransferReserveToBuffer(ctx context.Context, msg *types.MsgTransferReserveToBuffer) (*types.MsgTransferReserveToBufferResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil reserve transfer message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := validatePositiveNoahCoin(msg.Amount); err != nil {
		return nil, fmt.Errorf("invalid transfer amount: %w", err)
	}
	if err := msg.MinimumReserveBalance.Validate(); err != nil {
		return nil, fmt.Errorf("invalid minimum reserve balance: %w", err)
	}
	if msg.MinimumReserveBalance.Denom != chain.NoahBaseDenom {
		return nil, fmt.Errorf(
			"invalid minimum reserve balance: coin must be denominated in %s",
			chain.NoahBaseDenom,
		)
	}

	reserveBefore := m.k.balance(ctx, types.StrategicReserveName)
	reserveAfter, err := reserveBefore.SafeSub(msg.Amount.Amount)
	if err != nil || reserveAfter.LT(msg.MinimumReserveBalance.Amount) {
		return nil, fmt.Errorf(
			"reserve balance %s cannot fund %s while retaining %s",
			reserveBefore,
			msg.Amount,
			msg.MinimumReserveBalance,
		)
	}

	if err := m.k.bankKeeper.SendCoinsFromModuleToModule(
		ctx,
		types.StrategicReserveName,
		types.RedemptionBufferName,
		sdk.NewCoins(msg.Amount),
	); err != nil {
		return nil, fmt.Errorf("transferring reserve to redemption buffer: %w", err)
	}

	return &types.MsgTransferReserveToBufferResponse{}, nil
}
