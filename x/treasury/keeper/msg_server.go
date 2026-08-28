package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

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
	if msg.Params.ReferenceTaxCap.Denom != current.ReferenceTaxCap.Denom {
		return nil, sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"reference tax cap denom is set by the protocol reference: re-point it with MsgSetReferenceDenom, not %s",
			msg.Params.ReferenceTaxCap.Denom,
		)
	}
	if !current.ReferenceTaxCap.Equal(msg.Params.ReferenceTaxCap) {
		denoms, err := m.k.assetKeeper.OraclePricedDenoms(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting oracle-priced denominations: %w", err)
		}
		caps, underived, err := m.k.buildTaxCaps(ctx, msg.Params, denoms)
		if err != nil {
			return nil, err
		}
		for _, cap := range caps {
			if err := m.k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
				return nil, fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
			}
		}
		if err := m.k.TaxCapRefreshPending.Set(ctx, len(underived) > 0); err != nil {
			return nil, fmt.Errorf("recording pending tax cap refresh: %w", err)
		}
		if len(caps) > 0 {
			if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventTaxCapsRefreshed{
				TaxCaps: caps,
			}); err != nil {
				return nil, fmt.Errorf("emitting Treasury tax-cap refresh event: %w", err)
			}
		}
	}
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
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
	envelope, committeeAddress, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight)
	if err != nil {
		return nil, err
	}
	// A disabling has no committee to look up.
	if !envelope.IsDisabled() {
		envelope.Observe(m.k.accountKeeper.GetAccount(ctx, committeeAddress))
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
		CommitteeShape:   updated.CommitteeShape,
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
	if err := m.k.MonetaryPolicy.Set(ctx, msg.Policy); err != nil {
		return nil, fmt.Errorf("setting monetary policy: %w", err)
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
	if err := m.k.MonetaryPolicy.Set(ctx, msg.Policy); err != nil {
		return nil, fmt.Errorf("setting monetary policy: %w", err)
	}
	return &types.MsgCommitteeUpdatePolicyResponse{}, nil
}
