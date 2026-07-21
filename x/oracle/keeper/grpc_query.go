package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

var _ types.QueryServer = (*queryServer)(nil)

type queryServer struct {
	types.UnimplementedQueryServer

	k *Keeper
}

// NewQueryServerImpl returns an oracle QueryServer.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Params queries oracle params.
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting oracle params: %v", err)
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// ExchangeRate queries the exchange rate for a denom.
func (q queryServer) ExchangeRate(ctx context.Context, req *types.QueryExchangeRateRequest) (*types.QueryExchangeRateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid denom %q: %v", req.Denom, err)
	}

	exchangeRate, err := q.k.GetExchangeRate(ctx, req.Denom)
	if err != nil {
		switch {
		case errors.Is(err, types.ErrUnknownDenom):
			return nil, status.Errorf(codes.NotFound, "exchange rate not found for denom %s", req.Denom)
		case errors.Is(err, types.ErrStaleExchangeRate):
			return nil, status.Errorf(codes.FailedPrecondition, "exchange rate stale for denom %s: %v", req.Denom, err)
		default:
			return nil, status.Errorf(codes.Internal, "getting exchange rate for denom %s: %v", req.Denom, err)
		}
	}

	return &types.QueryExchangeRateResponse{ExchangeRate: exchangeRate}, nil
}

// ExchangeRates queries all exchange rates.
func (q queryServer) ExchangeRates(ctx context.Context, req *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	exchangeRates, err := q.k.GetExchangeRates(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting oracle exchange rates: %v", err)
	}

	return &types.QueryExchangeRatesResponse{ExchangeRates: exchangeRates}, nil
}

// TobinTax queries the active Tobin tax for a denom.
func (q queryServer) TobinTax(ctx context.Context, req *types.QueryTobinTaxRequest) (*types.QueryTobinTaxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid denom %q: %v", req.Denom, err)
	}

	tobinTax, err := q.k.GetTobinTax(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, types.ErrUnknownDenom) {
			return nil, status.Errorf(codes.NotFound, "tobin tax not found for denom %s", req.Denom)
		}
		return nil, status.Errorf(codes.Internal, "getting tobin tax for denom %s: %v", req.Denom, err)
	}

	return &types.QueryTobinTaxResponse{TobinTax: tobinTax}, nil
}

// TobinTaxes queries all active Tobin taxes.
func (q queryServer) TobinTaxes(ctx context.Context, req *types.QueryTobinTaxesRequest) (*types.QueryTobinTaxesResponse, error) {
	tobinTaxes, err := q.k.GetTobinTaxes(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting oracle tobin taxes: %v", err)
	}

	return &types.QueryTobinTaxesResponse{TobinTaxes: tobinTaxes}, nil
}

// VoteTargets queries active vote target denoms.
func (q queryServer) VoteTargets(ctx context.Context, req *types.QueryVoteTargetsRequest) (*types.QueryVoteTargetsResponse, error) {
	voteTargets, err := q.k.GetVoteTargets(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting oracle vote targets: %v", err)
	}

	return &types.QueryVoteTargetsResponse{VoteTargets: voteTargets}, nil
}

// RewardWeight queries a validator's oracle reward weight.
func (q queryServer) RewardWeight(ctx context.Context, req *types.QueryRewardWeightRequest) (*types.QueryRewardWeightResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address %q: %v", req.ValidatorAddr, err)
	}
	rewardWeight, err := q.k.RewardWeight.Get(ctx, valAddr)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.Internal, "getting reward weight for validator %s: %v", valAddr, err)
		}
		rewardWeight = math.ZeroInt()
	}

	return &types.QueryRewardWeightResponse{RewardWeight: rewardWeight}, nil
}

// MissCount queries a validator's oracle miss count.
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
