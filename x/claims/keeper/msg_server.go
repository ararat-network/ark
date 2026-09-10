package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/claims/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns the Claims message server.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// UpdateParams replaces the complete governance-owned Claims parameter set.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update params message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting Claims params: %w", err)
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// SetClaimsMandate appoints, replaces, or disables the Claims committee.
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
	envelope, committeeAddress, err := mandate.Next(
		current.Envelope,
		msg.Committee,
		msg.ActivationHeight,
		msg.ExpiryHeight,
	)
	if err != nil {
		return nil, err
	}
	// A disabling has no committee to look up.
	if !envelope.IsDisabled() {
		envelope.Observe(
			m.k.accountKeeper.GetAccount(ctx, committeeAddress),
			m.k.wasmKeeper.HasContractInfo(ctx, committeeAddress),
		)
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
		return nil, errors.New("Claims committee must be distinct from the Claims authority")
	}
	if msg.Committee != "" {
		// An appointment whose window has already closed could never act: the
		// message would only advance the term, reset the allowance, and unseat
		// the incumbent committee. Heights drift between drafting a proposal and
		// executing it, so an elapsed window is a drafting failure to surface,
		// not intent to honour.
		if height := uint64(sdkCtx.BlockHeight()); msg.ExpiryHeight <= height {
			return nil, fmt.Errorf(
				"Claims mandate expiry height %d is not after current height %d",
				msg.ExpiryHeight,
				height,
			)
		}
		params, err := m.k.Params.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting Claims params: %w", err)
		}
		// The envelope validated activation < expiry, so the span subtraction
		// cannot underflow. A span shorter than the cancellation period would
		// leave the committee unable to submit any claim for the whole
		// appointment, because every closing height would land past expiry.
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
		CommitteeShape:   claimsMandate.CommitteeShape,
	}); err != nil {
		return nil, fmt.Errorf("emitting Claims mandate: %w", err)
	}
	return &types.MsgSetClaimsMandateResponse{}, nil
}

// SubmitClaim records one governance-submitted pending claim. Governance
// submissions never consume the committee term allowance and do not depend on
// the Claims mandate; their cancellation period comes from Claims params.
func (m msgServer) SubmitClaim(ctx context.Context, msg *types.MsgSubmitClaim) (*types.MsgSubmitClaimResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil submit claim message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	claimID, err := m.k.submitClaim(ctx, claimSubmission{
		Submitter: msg.Authority,
		Origin:    types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE,
		Reference: msg.Reference,
		Recipient: msg.Recipient,
		Amount:    msg.Amount,
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
	claimsMandate, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	claimID, err := m.k.submitClaim(ctx, claimSubmission{
		Submitter:           msg.Committee,
		Origin:              types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE,
		MandateTerm:         claimsMandate.Term,
		MandateExpiryHeight: claimsMandate.ExpiryHeight,
		CommitteeClaimLimit: claimsMandate.CommitteeClaimLimit,
		Reference:           msg.Reference,
		Recipient:           msg.Recipient,
		Amount:              msg.Amount,
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
	if err := m.k.cancelClaim(ctx, types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE, msg.ClaimId); err != nil {
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
	if _, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm); err != nil {
		return nil, err
	}

	if err := m.k.cancelClaim(ctx, types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE, msg.ClaimId); err != nil {
		return nil, err
	}
	return &types.MsgCommitteeCancelClaimResponse{}, nil
}
