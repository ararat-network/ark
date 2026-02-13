package keeper

import (
	"context"

	marketv1 "noah/api/noah/market/v1"

	base "cosmossdk.io/api/cosmos/base/v1beta1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ marketv1.QueryServer = queryServer{}

type queryServer struct {
	k Keeper
	marketv1.UnimplementedQueryServer
}

func NewQueryServerImpl(k Keeper) marketv1.QueryServer {
	return queryServer{k: k}
}

// Params queries params of market module
func (q queryServer) Params(ctx context.Context, _ *marketv1.QueryParamsRequest) (*marketv1.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &marketv1.QueryParamsResponse{Params: params}, nil
}

// Swap queries for swap simulation
func (q queryServer) Swap(ctx context.Context, req *marketv1.QuerySwapRequest) (*marketv1.QuerySwapResponse, error) {
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

	retCoin, err := q.k.simulateSwap(ctx, offerCoin, req.AskDenom)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &marketv1.QuerySwapResponse{
		ReturnCoin: &base.Coin{
			Denom:  retCoin.Denom,
			Amount: retCoin.Amount.String(),
		},
	}, nil
}

// NoahPoolDelta queries noah pool delta
func (q queryServer) NoahPoolDelta(ctx context.Context, req *marketv1.QueryNoahPoolDeltaRequest) (*marketv1.QueryNoahPoolDeltaResponse, error) {
	noahPoolDelta, err := q.k.NoahPoolDelta.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &marketv1.QueryNoahPoolDeltaResponse{NoahPoolDelta: noahPoolDelta.String()}, nil
}
