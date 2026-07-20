package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
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

	referenceChanged := !current.ReferenceTaxCap.Equal(msg.Params.ReferenceTaxCap)
	var caps []types.TaxCap
	if referenceChanged {
		caps, err = m.k.BuildTaxCaps(ctx, msg.Params)
		if err != nil {
			return nil, err
		}
	}
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
	}
	if referenceChanged {
		if err := m.k.ReplaceTaxCaps(ctx, caps); err != nil {
			return nil, err
		}
		if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventTaxCapsUpdated{
			TaxCaps: caps,
		}); err != nil {
			return nil, fmt.Errorf("emitting Treasury tax-cap update event: %w", err)
		}
	}
	return &types.MsgUpdateParamsResponse{}, nil
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
	if msg.MinimumReserveBalance.Denom != chain.MicroNoahDenom {
		return nil, fmt.Errorf(
			"invalid minimum reserve balance: coin must be denominated in %s",
			chain.MicroNoahDenom,
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
