package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/security/types"
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
		msg.ActivationTime,
		msg.ExpiryTime,
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

	updated := types.NewDisabledSecurityMandate(envelope.Term)
	updated.Envelope = envelope
	if msg.Committee != "" {
		if err := updated.Validate(); err != nil {
			return nil, err
		}
		// Governance already holds the unbounded path, so appointing itself delegates nothing.
		if updated.Committee == m.k.authority || updated.Committee == msg.Authority {
			return nil, errors.New("security committee must be distinct from the chain authority")
		}
		// An expired window authorises nothing and would only displace the incumbent and advance the
		// term. Genesis skips this check, so an export taken after expiry round-trips.
		if now := chain.BlockSeconds(ctx); updated.ExpiryTime <= now {
			return nil, fmt.Errorf(
				"%s expires at time %d, which is not after the current block time %d",
				types.SecurityMandateLabel,
				updated.ExpiryTime,
				now,
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
		Term:           updated.Term,
		Committee:      updated.Committee,
		ActivationTime: updated.ActivationTime,
		ExpiryTime:     updated.ExpiryTime,
		CommitteeShape: updated.CommitteeShape,
	}); err != nil {
		return nil, fmt.Errorf("emitting security mandate: %w", err)
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
	securityMandate, err := m.k.AuthoriseCommittee(
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
	if msg.Height > types.MaxPlanHeight {
		return nil, fmt.Errorf("upgrade height %d exceeds x/upgrade's maximum %d", msg.Height, types.MaxPlanHeight)
	}
	height := int64(msg.Height)

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
		// x/upgrade accepts the same plan again, and this module would then record
		// and announce a schedule that changed nothing.
		if pending.Name == msg.Name && pending.Height == height && pending.Info == msg.Info {
			return nil, fmt.Errorf("upgrade %s is already pending at height %d", pending.Name, pending.Height)
		}
	}

	if err := m.k.dispatch(ctx, &upgradetypes.MsgSoftwareUpgrade{
		Authority: m.k.effectiveAuthority(ctx),
		Plan: upgradetypes.Plan{
			Name:   msg.Name,
			Height: height,
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

// CommitteeCancelUpgrade requires the live committee term/window and a pending plan recorded as
// committee-owned. Governance plans cannot be cancelled through this path.
func (m msgServer) CommitteeCancelUpgrade(ctx context.Context, msg *types.MsgCommitteeCancelUpgrade) (*types.MsgCommitteeCancelUpgradeResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee cancel upgrade message")
	}
	securityMandate, err := m.k.AuthoriseCommittee(
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
		Height: uint64(pending.Height),
	}); err != nil {
		return nil, err
	}

	return &types.MsgCommitteeCancelUpgradeResponse{}, nil
}
