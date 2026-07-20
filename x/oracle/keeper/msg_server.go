package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

var _ types.MsgServer = (*msgServer)(nil)

type msgServer struct {
	types.UnimplementedMsgServer

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

	currentParams, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting current params: %w", err)
	}
	currentDenoms := make(map[string]struct{}, len(currentParams.TobinTaxes))
	for _, tobinTax := range currentParams.TobinTaxes {
		currentDenoms[tobinTax.Denom] = struct{}{}
	}
	for _, tobinTax := range msg.Params.TobinTaxes {
		if _, ok := currentDenoms[tobinTax.Denom]; !ok {
			m.k.registerTobinTaxMetadata(ctx, tobinTax.Denom)
		}
	}

	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
