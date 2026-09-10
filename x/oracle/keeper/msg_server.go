package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/x/oracle/types"
)

var _ types.MsgServer = (*msgServer)(nil)

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

	// Oracle parameters carry no feed referents: membership moves only through
	// MsgAddFeed and MsgRemoveFeed, and every consumer-side referent (the asset
	// registry and the protocol reference) is checked by the removal guard.
	if err := m.k.Params.Set(ctx, msg.Params); err != nil {
		return nil, fmt.Errorf("setting params: %w", err)
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

// AddFeed schedules feed membership, accepting already active or scheduled additions. Invalid
// denominations and conflicting transitions fail.
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

// SetReferenceDenom re-points the protocol reference unit.
func (m msgServer) SetReferenceDenom(ctx context.Context, msg *types.MsgSetReferenceDenom) (*types.MsgSetReferenceDenomResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdk.ValidateAuthority(sdkCtx, m.k.authority, msg.Authority); err != nil {
		return nil, err
	}

	// An unset decimal round-trips through amino JSON as zero, so absent and
	// zero have to mean the same thing: convert at the stored rate. A rate of
	// zero could not mean anything else, since nothing can be converted out of
	// a unit worth nothing.
	outgoingRate := msg.OutgoingRate
	if !outgoingRate.IsNil() && outgoingRate.IsZero() {
		outgoingRate = math.LegacyDec{}
	}
	if !outgoingRate.IsNil() && (!outgoingRate.IsPositive() || outgoingRate.GT(types.MaxOutgoingReferenceRate)) {
		return nil, errorsmod.Wrapf(
			errortypes.ErrInvalidRequest,
			"outgoing reference denom rate must be positive and at most %s, is %s",
			types.MaxOutgoingReferenceRate,
			outgoingRate,
		)
	}
	if err := m.k.SetReferenceDenom(ctx, msg.ReferenceDenom, outgoingRate); err != nil {
		return nil, err
	}

	return &types.MsgSetReferenceDenomResponse{}, nil
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
