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

	// Oracle parameters carry no feed referents: membership moves only through
	// MsgAddFeed and MsgRemoveFeed, and every consumer-side referent (the asset
	// registry and the protocol reference) is checked by the removal guard.
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// AddFeed schedules one feed addition.
func (m msgServer) AddFeed(ctx context.Context, msg *types.MsgAddFeed) (*types.MsgAddFeedResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	if err := m.k.ScheduleFeedTransition(
		ctx,
		msg.Denom,
		types.FeedDirection_FEED_DIRECTION_ADD,
	); err != nil {
		return nil, err
	}

	return &types.MsgAddFeedResponse{}, nil
}

// RemoveFeed schedules one feed removal once no consumer still references it.
func (m msgServer) RemoveFeed(ctx context.Context, msg *types.MsgRemoveFeed) (*types.MsgRemoveFeedResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	if err := m.k.requireFeedUnreferenced(ctx, msg.Denom); err != nil {
		return nil, err
	}
	if err := m.k.ScheduleFeedTransition(
		ctx,
		msg.Denom,
		types.FeedDirection_FEED_DIRECTION_REMOVE,
	); err != nil {
		return nil, err
	}

	return &types.MsgRemoveFeedResponse{}, nil
}
