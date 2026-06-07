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

// NewMsgServerImpl returns an implementation of the oracle MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(k *Keeper) types.MsgServer {
	return &msgServer{k: k}
}

// UpdateParams updates the params.
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	if err := msg.Params.Validate(); err != nil {
		return nil, err
	}

	oldParams, err := m.k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, err
	}

	if !tobinTaxesEqual(oldParams.TobinTaxes, msg.Params.TobinTaxes) {
		if err := m.k.ApplyTobinTaxChanges(ctx, msg.Params.TobinTaxes); err != nil {
			return nil, err
		}
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

func tobinTaxesEqual(a, b types.TobinTaxes) bool {
	if len(a) != len(b) {
		return false
	}

	taxes := make(map[string]types.TobinTax, len(a))
	for _, item := range a {
		taxes[item.Denom] = item
	}
	if len(taxes) != len(a) {
		return false
	}

	for _, item := range b {
		existing, ok := taxes[item.Denom]
		if !ok || !existing.Equal(item) {
			return false
		}
		delete(taxes, item.Denom)
	}

	return len(taxes) == 0
}
