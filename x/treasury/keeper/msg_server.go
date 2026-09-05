package keeper

import (
	"context"
	"errors"
	"fmt"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/treasury/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
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
	if msg.Params.ReferenceDenom != current.ReferenceDenom {
		return nil, errorsmod.Wrapf(
			errortypes.ErrInvalidRequest,
			"reference denom is set by the protocol reference: re-point it with MsgSetReferenceDenom, not %s",
			msg.Params.ReferenceDenom,
		)
	}
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
	}
	return &types.MsgUpdateParamsResponse{}, nil
}

// SetEconomicMandate replaces or disables the bounded committee
// appointment. Treasury derives a new term for every replacement.
func (m msgServer) SetEconomicMandate(ctx context.Context, msg *types.MsgSetEconomicMandate) (*types.MsgSetEconomicMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set economic mandate message")
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	current, err := m.k.EconomicMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting economic mandate: %w", err)
	}
	envelope, committeeAddress, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight)
	if err != nil {
		return nil, err
	}
	// A disabling has no committee to look up.
	if !envelope.IsDisabled() {
		envelope.Observe(m.k.accountKeeper.GetAccount(ctx, committeeAddress))
	}
	updated := types.NewDisabledEconomicMandate(envelope.Term)
	updated.Envelope = envelope
	if msg.Committee != "" {
		updated.MinimumPolicy = msg.MinimumPolicy
		updated.MaximumPolicy = msg.MaximumPolicy
		if err := updated.Validate(); err != nil {
			return nil, err
		}
		if updated.Committee == m.k.authority || updated.Committee == msg.Authority {
			return nil, errors.New("economic-policy committee must be distinct from Treasury authority")
		}
	}

	if err := m.k.EconomicMandate.Set(ctx, updated); err != nil {
		return nil, fmt.Errorf("setting economic mandate: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventEconomicMandateSet{
		Term:             updated.Term,
		Committee:        updated.Committee,
		ActivationHeight: updated.ActivationHeight,
		ExpiryHeight:     updated.ExpiryHeight,
		CommitteeShape:   updated.CommitteeShape,
	}); err != nil {
		return nil, fmt.Errorf("emitting Treasury economic mandate: %w", err)
	}
	return &types.MsgSetEconomicMandateResponse{}, nil
}

// UpdatePolicy applies one complete reversible policy update as the
// governance authority. Governance overrides the committee mandate, so no term,
// window, or policy-bound check applies.
func (m msgServer) UpdatePolicy(ctx context.Context, msg *types.MsgUpdatePolicy) (*types.MsgUpdatePolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update economic-policy message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}
	if err := m.k.EconomicPolicy.Set(ctx, msg.Policy); err != nil {
		return nil, fmt.Errorf("setting economic policy: %w", err)
	}
	return &types.MsgUpdatePolicyResponse{}, nil
}

// CommitteeUpdatePolicy applies one complete reversible policy update
// as the exact appointed committee, during the active term and window, with
// every field inside the mandate's bounds.
func (m msgServer) CommitteeUpdatePolicy(ctx context.Context, msg *types.MsgCommitteeUpdatePolicy) (*types.MsgCommitteeUpdatePolicyResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil committee update economic-policy message")
	}
	if err := msg.Policy.Validate(); err != nil {
		return nil, err
	}

	mandate, err := m.k.AuthoriseCommittee(ctx, msg.Committee, msg.ExpectedTerm)
	if err != nil {
		return nil, err
	}
	if err := mandate.ValidatePolicy(msg.Policy); err != nil {
		return nil, err
	}
	if err := m.k.EconomicPolicy.Set(ctx, msg.Policy); err != nil {
		return nil, fmt.Errorf("setting economic policy: %w", err)
	}
	return &types.MsgCommitteeUpdatePolicyResponse{}, nil
}
