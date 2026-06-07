package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns an implementation of the oracle QueryServer interface
// for the provided Keeper.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Params queries params of distribution module
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting oracle params: %v", err)
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// ExchangeRate queries exchange rate of a denom
func (q queryServer) ExchangeRate(ctx context.Context, req *types.QueryExchangeRateRequest) (*types.QueryExchangeRateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid denom %q: %v", req.Denom, err)
	}

	if req.Denom == core.MicroArkDenom {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		return &types.QueryExchangeRateResponse{
			ExchangeRate: types.NewExchangeRate(
				req.Denom,
				math.LegacyOneDec(),
				sdkCtx.BlockTime(),
				uint64(sdkCtx.BlockHeight()),
			),
		}, nil
	}

	exchangeRate, err := q.k.ExchangeRate.Get(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "exchange rate not found for denom %s", req.Denom)
		}
		return nil, status.Errorf(codes.Internal, "getting exchange rate for denom %s: %v", req.Denom, err)
	}

	return &types.QueryExchangeRateResponse{ExchangeRate: exchangeRate}, nil
}

// ExchangeRates queries exchange rates of all denoms
func (q queryServer) ExchangeRates(ctx context.Context, req *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	var exchangeRates types.ExchangeRates
	if err := q.k.ExchangeRate.Walk(ctx, nil, func(_ string, exchangeRate types.ExchangeRate) (bool, error) {
		exchangeRates = append(exchangeRates, exchangeRate)
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing oracle exchange rates: %v", err)
	}

	return &types.QueryExchangeRatesResponse{ExchangeRates: exchangeRates}, nil
}

// TobinTax queries tobin tax of a denom
func (q queryServer) TobinTax(ctx context.Context, req *types.QueryTobinTaxRequest) (*types.QueryTobinTaxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid denom %q: %v", req.Denom, err)
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting params: %v", err)
	}
	for _, tt := range params.TobinTaxes {
		if tt.Denom == req.Denom {
			return &types.QueryTobinTaxResponse{TobinTax: tt.TobinTax}, nil
		}
	}

	return nil, status.Errorf(codes.NotFound, "tobin tax not found for denom %s", req.Denom)
}

// TobinTaxes queries tobin taxes of all denoms
func (q queryServer) TobinTaxes(ctx context.Context, req *types.QueryTobinTaxesRequest) (*types.QueryTobinTaxesResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting params: %v", err)
	}

	return &types.QueryTobinTaxesResponse{TobinTaxes: params.TobinTaxes}, nil
}

// Actives queries all denoms for which exchange rates exist
func (q queryServer) Actives(ctx context.Context, req *types.QueryActivesRequest) (*types.QueryActivesResponse, error) {
	actives, err := q.k.GetActives(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting active oracle denoms: %v", err)
	}

	return &types.QueryActivesResponse{Actives: actives}, nil
}

// VoteTargets queries the voting target list on current vote period
func (q queryServer) VoteTargets(ctx context.Context, req *types.QueryVoteTargetsRequest) (*types.QueryVoteTargetsResponse, error) {
	var voteTargets []string
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting params: %v", err)
	}
	for _, tt := range params.TobinTaxes {
		voteTargets = append(voteTargets, tt.Denom)
	}

	return &types.QueryVoteTargetsResponse{VoteTargets: voteTargets}, nil
}

// ScoreWeight queries oracle score weight of a validator
func (q queryServer) ScoreWeight(ctx context.Context, req *types.QueryScoreWeightRequest) (*types.QueryScoreWeightResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address %q: %v", req.ValidatorAddr, err)
	}
	score, err := q.k.ScoreWeight.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "getting score weight for validator %s: %v", valAddr, err)
	}

	return &types.QueryScoreWeightResponse{ScoreWeight: score}, nil
}

// MissCount queries oracle miss count of a validator
func (q queryServer) MissCount(ctx context.Context, req *types.QueryMissCountRequest) (*types.QueryMissCountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address %q: %v", req.ValidatorAddr, err)
	}
	missCount, err := q.k.MissCount.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "getting miss count for validator %s: %v", valAddr, err)
	}

	return &types.QueryMissCountResponse{MissCount: missCount}, nil
}
