package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/asset/types"
)

var _ types.MsgServer = (*msgServer)(nil)

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns an asset MsgServer.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{k: k}
}

// UpdateParams replaces the governance-owned asset module parameters.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil update params message")
	}
	if err := sdk.ValidateAuthority(
		sdk.UnwrapSDKContext(ctx),
		m.k.authority,
		msg.Authority,
	); err != nil {
		return nil, err
	}
	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}
	// A settlement plan already open keeps the delay it was opened under: the
	// delay bounds when a plan may activate, and it is checked once, when the
	// plan is written. Changing it here moves the floor for plans opened from
	// now on and never reaches terms holders have already been shown.
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting asset params: %w", err)
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

func (m msgServer) RegisterAsset(ctx context.Context, msg *types.MsgRegisterAsset) (*types.MsgRegisterAssetResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil register asset message")
	}
	if err := sdk.ValidateAuthority(
		sdk.UnwrapSDKContext(ctx),
		m.k.authority,
		msg.Authority,
	); err != nil {
		return nil, err
	}
	if err := m.k.RegisterAsset(ctx, msg.Denom); err != nil {
		return nil, err
	}

	return &types.MsgRegisterAssetResponse{}, nil
}

func (m msgServer) HaltIssuance(ctx context.Context, msg *types.MsgHaltIssuance) (*types.MsgHaltIssuanceResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil halt issuance message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.HaltIssuance(ctx, msg.Denom, msg.ExpectedVersion); err != nil {
		return nil, err
	}

	return &types.MsgHaltIssuanceResponse{}, nil
}

func (m msgServer) ResumeIssuance(ctx context.Context, msg *types.MsgResumeIssuance) (*types.MsgResumeIssuanceResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil resume issuance message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.ResumeIssuance(ctx, msg.Denom, msg.ExpectedVersion); err != nil {
		return nil, err
	}

	return &types.MsgResumeIssuanceResponse{}, nil
}

func (m msgServer) SuspendAsset(ctx context.Context, msg *types.MsgSuspendAsset) (*types.MsgSuspendAssetResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil suspend asset message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.SuspendAsset(ctx, msg.Denom, msg.ExpectedVersion); err != nil {
		return nil, err
	}

	return &types.MsgSuspendAssetResponse{}, nil
}

func (m msgServer) OpenSettlement(ctx context.Context, msg *types.MsgOpenSettlement) (*types.MsgOpenSettlementResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil open settlement message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.OpenSettlement(
		ctx,
		msg.Denom,
		msg.ExpectedVersion,
		msg.RedemptionRate,
		msg.EarliestClosingHeight,
	); err != nil {
		return nil, err
	}

	return &types.MsgOpenSettlementResponse{}, nil
}

func (m msgServer) CancelSettlement(ctx context.Context, msg *types.MsgCancelSettlement) (*types.MsgCancelSettlementResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil cancel settlement message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.CancelSettlement(ctx, msg.Denom, msg.ExpectedVersion); err != nil {
		return nil, err
	}

	return &types.MsgCancelSettlementResponse{}, nil
}

func (m msgServer) RecoverAsset(ctx context.Context, msg *types.MsgRecoverAsset) (*types.MsgRecoverAssetResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil recover asset message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.RecoverAsset(ctx, msg.Denom, msg.ExpectedVersion); err != nil {
		return nil, err
	}

	return &types.MsgRecoverAssetResponse{}, nil
}

func (m msgServer) WriteOffAsset(ctx context.Context, msg *types.MsgWriteOffAsset) (*types.MsgWriteOffAssetResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil write off asset message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.WriteOffAsset(ctx, msg.Denom, msg.ExpectedVersion); err != nil {
		return nil, err
	}

	return &types.MsgWriteOffAssetResponse{}, nil
}

func (m msgServer) FinalizeRetirement(ctx context.Context, msg *types.MsgFinalizeRetirement) (*types.MsgFinalizeRetirementResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil finalise retirement message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	if err := m.k.FinalizeRetirement(ctx, msg.Denom, msg.ExpectedVersion, msg.MaxResidualSupply); err != nil {
		return nil, err
	}

	return &types.MsgFinalizeRetirementResponse{}, nil
}

func (m msgServer) SetEmergencyMandate(ctx context.Context, msg *types.MsgSetEmergencyMandate) (*types.MsgSetEmergencyMandateResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil set emergency mandate message")
	}
	if err := sdk.ValidateAuthority(sdk.UnwrapSDKContext(ctx), m.k.authority, msg.Authority); err != nil {
		return nil, err
	}
	// Distinctness is judged before Next canonicalises the committee, so it must
	// compare the canonical spelling itself or a re-cased authority would pass.
	committee := msg.Committee
	if committee != "" {
		var err error
		if committee, err = chain.CanonicaliseAccountAddress("committee", committee); err != nil {
			return nil, err
		}
	}
	if committee == m.k.authority || committee == msg.Authority {
		return nil, errors.New("emergency committee must be distinct from the Asset authority")
	}
	if err := m.k.SetEmergencyMandate(ctx, committee, msg.ActivationHeight, msg.ExpiryHeight); err != nil {
		return nil, err
	}

	return &types.MsgSetEmergencyMandateResponse{}, nil
}

func (m msgServer) EmergencySuspendAsset(ctx context.Context, msg *types.MsgEmergencySuspendAsset) (*types.MsgEmergencySuspendAssetResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil emergency suspend asset message")
	}
	if err := m.k.EmergencySuspendAsset(ctx, msg.Committee, msg.Denom, msg.ExpectedTerm); err != nil {
		return nil, err
	}

	return &types.MsgEmergencySuspendAssetResponse{}, nil
}
