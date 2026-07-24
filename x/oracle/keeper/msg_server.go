package keeper

import (
	"context"
	"fmt"
	"slices"

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
	currentVoteTargets := types.VoteTargetDenoms(currentParams)
	nextVoteTargets := types.VoteTargetDenoms(msg.Params)
	for _, denom := range currentVoteTargets {
		if _, found := slices.BinarySearch(nextVoteTargets, denom); !found {
			return nil, fmt.Errorf("%w: %s", types.ErrVoteTargetRemoval, denom)
		}
	}

	for _, tobinTax := range msg.Params.TobinTaxes {
		if _, found := slices.BinarySearch(currentVoteTargets, tobinTax.Denom); !found {
			m.k.registerTobinTaxMetadata(ctx, tobinTax.Denom)
		}
	}
	if !slices.Equal(currentVoteTargets, nextVoteTargets) {
		if err := m.k.ScheduleVoteTargets(ctx, nextVoteTargets); err != nil {
			return nil, fmt.Errorf("scheduling vote targets: %w", err)
		}
	}

	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
