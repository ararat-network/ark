package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	types.UnimplementedQueryServer

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

	quote, err := q.k.quoteSwap(ctx, offerCoin, req.AskDenom)
	if err != nil {
		switch {
		case errors.Is(err, types.ErrNoEffectivePrice), errors.Is(err, oracletypes.ErrStaleExchangeRate):
			return nil, status.Errorf(codes.FailedPrecondition, "computing swap from %s to %s: %v", offerCoin, req.AskDenom, err)
		case errors.Is(err, errortypes.ErrInvalidCoins),
			errors.Is(err, errortypes.ErrInvalidRequest),
			errors.Is(err, types.ErrRecursiveSwap),
			errors.Is(err, types.ErrZeroSwapCoin):
			return nil, status.Errorf(codes.InvalidArgument, "computing swap from %s to %s: %v", offerCoin, req.AskDenom, err)
		case errors.Is(err, types.ErrArithmeticOutOfRange), errors.Is(err, oracletypes.ErrConversionOutOfRange):
			return nil, status.Errorf(codes.OutOfRange, "computing swap from %s to %s: %v", offerCoin, req.AskDenom, err)
		default:
			return nil, status.Errorf(codes.Internal, "computing swap from %s to %s: %v", offerCoin, req.AskDenom, err)
		}
	}

	return &types.QuerySwapResponse{
		SwapCoin: quote.swapCoin,
		SwapFee:  quote.swapFee,
	}, nil
}

// ArkPoolDelta queries ark pool delta
func (q queryServer) ArkPoolDelta(ctx context.Context, _ *types.QueryArkPoolDeltaRequest) (*types.QueryArkPoolDeltaResponse, error) {
	arkPoolDelta, err := q.k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market ark pool delta: %v", err)
	}
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting market params: %v", err)
	}
	return &types.QueryArkPoolDeltaResponse{
		ArkPoolDelta: arkPoolDelta,
		PoolDenom:    params.BasePool.Denom,
	}, nil
}
