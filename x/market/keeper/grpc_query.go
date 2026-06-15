package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"noah/x/market/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k *Keeper
}

func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Params queries params of market module
func (q queryServer) Params(ctx context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market params: %v", err)
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// Swap queries for swap simulation
func (q queryServer) Swap(ctx context.Context, req *types.QuerySwapRequest) (*types.QuerySwapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	offerCoin, err := sdk.ParseCoinNormalized(req.OfferCoin)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "parsing offer coin %q: %v", req.OfferCoin, err)
	}

	if err := validateInputs(offerCoin, req.AskDenom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "validating swap from %s to %s: %v", offerCoin, req.AskDenom, err)
	}

	swapDecCoin, spread, err := q.k.ComputeSwap(ctx, offerCoin, req.AskDenom)
	if err != nil {
		switch {
		case errors.Is(err, types.ErrNoEffectivePrice):
			return nil, status.Errorf(codes.FailedPrecondition, "computing swap from %s to %s: %v", offerCoin, req.AskDenom, err)
		case errors.Is(err, errortypes.ErrInvalidCoins), errors.Is(err, types.ErrRecursiveSwap):
			return nil, status.Errorf(codes.InvalidArgument, "computing swap from %s to %s: %v", offerCoin, req.AskDenom, err)
		default:
			return nil, status.Errorf(codes.Internal, "computing swap from %s to %s: %v", offerCoin, req.AskDenom, err)
		}
	}

	outcome, err := buildSwapOutcome(swapDecCoin, spread)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "building swap outcome for %s to %s: %v", offerCoin, req.AskDenom, err)
	}

	return &types.QuerySwapResponse{
		SwapCoin: outcome.swapCoin,
		SwapFee:  outcome.swapFee,
	}, nil
}

// NoahPoolDelta queries noah pool delta
func (q queryServer) NoahPoolDelta(ctx context.Context, _ *types.QueryNoahPoolDeltaRequest) (*types.QueryNoahPoolDeltaResponse, error) {
	noahPoolDelta, err := q.k.NoahPoolDelta.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market noah pool delta: %v", err)
	}
	return &types.QueryNoahPoolDeltaResponse{NoahPoolDelta: noahPoolDelta}, nil
}
