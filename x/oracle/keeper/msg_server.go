package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	k *Keeper
}

// NewMsgServerImpl returns an oracle MsgServer.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{k: k}
}

// UpdateParams updates oracle params.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
