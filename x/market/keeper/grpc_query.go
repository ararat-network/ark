package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

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
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// Swap queries for swap simulation
func (q queryServer) Swap(ctx context.Context, req *types.QuerySwapRequest) (*types.QuerySwapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if err := sdk.ValidateDenom(req.AskDenom); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid ask denom")
	}

	offerCoin, err := sdk.ParseCoinNormalized(req.OfferCoin)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := validateInputs(offerCoin, req.AskDenom); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	swapDecCoin, spread, err := q.k.ComputeSwap(ctx, offerCoin, req.AskDenom)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	outcome, err := buildSwapOutcome(swapDecCoin, spread)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
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
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryNoahPoolDeltaResponse{NoahPoolDelta: noahPoolDelta}, nil
}
