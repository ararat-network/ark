package keeper

import (
	"context"
	"errors"
	"fmt"

	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"ark/pkg/mandate"
	"ark/x/security/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns an implementation of the security MsgServer
// interface for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return msgServer{k: k}
}

// SetSecurityMandate replaces or disables the security committee appointment.
// Security derives a new term for every replacement, so transactions prepared
// against the outgoing appointment cannot become valid again.
func (m msgServer) SetSecurityMandate(ctx context.Context, msg *types.MsgSetSecurityMandate) (*types.MsgSetSecurityMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set security mandate message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	current, err := m.k.Mandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting security mandate: %w", err)
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
		envelope.Observe(m.k.accountKeeper.GetAccount(ctx, committeeAddress))
	}

	updated := types.NewDisabledSecurityMandate(envelope.Term)
	updated.Envelope = envelope
	if msg.Committee != "" {
		if err := updated.Validate(); err != nil {
			return nil, err
		}
		// A committee that is also the authority is a delegation to nobody that
		// still reads as a live fast path.
		if updated.Committee == m.k.authority || updated.Committee == msg.Authority {
			return nil, errors.New("security committee must be distinct from the chain authority")
		}
		// An expiry already behind the chain reads as live and authorises
		// nothing; the window is half-open, so landing on it is already too
		// late. Genesis applies no such check: an import must carry a mandate
		// that expired before the export.
		if updated.ExpiryHeight <= uint64(sdkCtx.BlockHeight()) {
			return nil, fmt.Errorf(
				"security mandate expires at height %d, which is not above the current height %d",
				updated.ExpiryHeight,
				sdkCtx.BlockHeight(),
			)
		}
	}

	if err := m.k.Mandate.Set(ctx, updated); err != nil {
		return nil, fmt.Errorf("setting security mandate: %w", err)
	}
	// The plan record is left alone: it describes a plan in x/upgrade that a new
	// appointment neither cancels nor inherits, so carrying it across tells the
	// next committee whose plan holds the slot.
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventSecurityMandateSet{
		Term:             updated.Term,
		Committee:        updated.Committee,
		ActivationHeight: updated.ActivationHeight,
		ExpiryHeight:     updated.ExpiryHeight,
		CommitteeShape:   updated.CommitteeShape,
	}); err != nil {
		return nil, err
	}

	return &types.MsgSetSecurityMandateResponse{}, nil
}

// CommitteePlanUpgrade schedules an emergency upgrade plan as the exact
// appointed committee, during the active term and window. It may replace only
// a plan this module recorded scheduling, because x/upgrade overwrites its
// single slot silently.
func (m msgServer) CommitteePlanUpgrade(ctx context.Context, msg *types.MsgCommitteePlanUpgrade) (*types.MsgCommitteePlanUpgradeResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee schedule upgrade message")
	}
	securityMandate, err := m.k.authoriseCommittee(
		ctx,
		msg.Committee,
		msg.ExpectedTerm,
	)
	if err != nil {
		return nil, err
	}
	if msg.Name == "" {
		return nil, errors.New("upgrade plan must have a name")
	}

	pending, hasPending, err := m.k.pendingUpgradePlan(ctx)
	if err != nil {
		return nil, err
	}
	if hasPending {
		ownPlan, err := m.k.isCommitteePlan(ctx, pending)
		if err != nil {
			return nil, err
		}
		if !ownPlan {
			return nil, fmt.Errorf(
				"upgrade %s is pending at height %d and was not scheduled by the committee: only governance replaces it",
				pending.Name,
				pending.Height,
			)
		}
	}

	if err := m.k.dispatch(ctx, &upgradetypes.MsgSoftwareUpgrade{
		Authority: m.k.effectiveAuthority(ctx),
		Plan: upgradetypes.Plan{
			Name:   msg.Name,
			Height: msg.Height,
			Info:   msg.Info,
		},
	}); err != nil {
		return nil, err
	}

	if err := m.k.CommitteePlan.Set(ctx, types.CommitteePlan{
		Name:   msg.Name,
		Height: msg.Height,
		Term:   securityMandate.Term,
	}); err != nil {
		return nil, fmt.Errorf("recording committee plan: %w", err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventCommitteeUpgradeScheduled{
		Term:   securityMandate.Term,
		Name:   msg.Name,
		Height: msg.Height,
	}); err != nil {
		return nil, err
	}

	return &types.MsgCommitteePlanUpgradeResponse{}, nil
}

// CommitteeCancelUpgrade cancels the pending upgrade plan as the exact
// appointed committee, during the active term and window. Only a plan this
// module recorded scheduling qualifies: cancelling a governance plan would
// clear the slot and let the committee schedule over a decision the chain
// voted on.
func (m msgServer) CommitteeCancelUpgrade(ctx context.Context, msg *types.MsgCommitteeCancelUpgrade) (*types.MsgCommitteeCancelUpgradeResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee cancel upgrade message")
	}
	securityMandate, err := m.k.authoriseCommittee(
		ctx,
		msg.Committee,
		msg.ExpectedTerm,
	)
	if err != nil {
		return nil, err
	}

	pending, hasPending, err := m.k.pendingUpgradePlan(ctx)
	if err != nil {
		return nil, err
	}
	if !hasPending {
		return nil, errors.New("no upgrade plan is pending")
	}
	ownPlan, err := m.k.isCommitteePlan(ctx, pending)
	if err != nil {
		return nil, err
	}
	if !ownPlan {
		return nil, fmt.Errorf(
			"upgrade %s is pending at height %d and was not scheduled by the committee: only governance cancels it",
			pending.Name,
			pending.Height,
		)
	}

	if err := m.k.dispatch(ctx, &upgradetypes.MsgCancelUpgrade{
		Authority: m.k.effectiveAuthority(ctx),
	}); err != nil {
		return nil, err
	}
	if err := m.k.CommitteePlan.Set(ctx, types.CommitteePlan{}); err != nil {
		return nil, fmt.Errorf("clearing committee plan record: %w", err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventCommitteeUpgradeCancelled{
		Term:   securityMandate.Term,
		Name:   pending.Name,
		Height: pending.Height,
	}); err != nil {
		return nil, err
	}

	return &types.MsgCommitteeCancelUpgradeResponse{}, nil
}

// CommitteeRecoverClient substitutes an expired or frozen IBC client as the
// exact appointed committee, during the active term and window.
func (m msgServer) CommitteeRecoverClient(ctx context.Context, msg *types.MsgCommitteeRecoverClient) (*types.MsgCommitteeRecoverClientResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee recover client message")
	}
	securityMandate, err := m.k.authoriseCommittee(
		ctx,
		msg.Committee,
		msg.ExpectedTerm,
	)
	if err != nil {
		return nil, err
	}
	if msg.SubjectClientId == "" || msg.SubstituteClientId == "" {
		return nil, errors.New("subject and substitute client identifiers must be set")
	}
	if msg.SubjectClientId == msg.SubstituteClientId {
		return nil, errors.New("subject and substitute clients must differ")
	}

	if err := m.k.dispatch(ctx, &clienttypes.MsgRecoverClient{
		SubjectClientId:    msg.SubjectClientId,
		SubstituteClientId: msg.SubstituteClientId,
		Signer:             m.k.effectiveAuthority(ctx),
	}); err != nil {
		return nil, err
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventCommitteeClientRecovered{
		Term:               securityMandate.Term,
		SubjectClientId:    msg.SubjectClientId,
		SubstituteClientId: msg.SubstituteClientId,
	}); err != nil {
		return nil, err
	}

	return &types.MsgCommitteeRecoverClientResponse{}, nil
}
