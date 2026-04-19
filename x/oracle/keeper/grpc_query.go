package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k *Keeper
	types.UnimplementedQueryServer
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
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// ExchangeRate queries exchange rate of a denom
func (q queryServer) ExchangeRate(ctx context.Context, req *types.QueryExchangeRateRequest) (*types.QueryExchangeRateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if len(req.Denom) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty denom")
	}

	exchangeRate, err := q.k.GetArkExchangeRate(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, req.Denom)
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryExchangeRateResponse{ExchangeRate: exchangeRate}, nil
}

// ExchangeRates queries exchange rates of all denoms
func (q queryServer) ExchangeRates(ctx context.Context, req *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	var exchangeRates sdk.DecCoins
	if err := q.k.ExchangeRate.Walk(ctx, nil, func(denom string, rate math.LegacyDec) (bool, error) {
		exchangeRates = append(exchangeRates, sdk.NewDecCoinFromDec(denom, rate))
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryExchangeRatesResponse{ExchangeRates: exchangeRates}, nil
}

// TobinTax queries tobin tax of a denom
func (q queryServer) TobinTax(ctx context.Context, req *types.QueryTobinTaxRequest) (*types.QueryTobinTaxResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if len(req.Denom) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty denom")
	}

	tobinTax, err := q.k.TobinTax.Get(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, req.Denom)
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTobinTaxResponse{TobinTax: tobinTax}, nil
}

// TobinTaxes queries tobin taxes of all denoms
func (q queryServer) TobinTaxes(ctx context.Context, req *types.QueryTobinTaxesRequest) (*types.QueryTobinTaxesResponse, error) {
	var tobinTaxes types.DenomList
	if err := q.k.TobinTax.Walk(ctx, nil, func(denom string, rate math.LegacyDec) (bool, error) {
		tobinTaxes = append(tobinTaxes, types.Denom{
			Name:     denom,
			TobinTax: rate,
		})
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTobinTaxesResponse{TobinTaxes: tobinTaxes}, nil
}

// Actives queries all denoms for which exchange rates exist
func (q queryServer) Actives(ctx context.Context, req *types.QueryActivesRequest) (*types.QueryActivesResponse, error) {
	var actives []string
	if err := q.k.ExchangeRate.Walk(ctx, nil, func(denom string, rate math.LegacyDec) (bool, error) {
		actives = append(actives, denom)
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryActivesResponse{Actives: actives}, nil
}

// VoteTargets queries the voting target list on current vote period
func (q queryServer) VoteTargets(ctx context.Context, req *types.QueryVoteTargetsRequest) (*types.QueryVoteTargetsResponse, error) {
	var voteTargets []string
	if err := q.k.TobinTax.Walk(ctx, nil, func(denom string, _ math.LegacyDec) (bool, error) {
		voteTargets = append(voteTargets, denom)
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryVoteTargetsResponse{VoteTargets: voteTargets}, nil
}

// FeederDelegation queries the account address that the validator operator delegated oracle vote rights to
func (q queryServer) FeederDelegation(ctx context.Context, req *types.QueryFeederDelegationRequest) (*types.QueryFeederDelegationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	feederAddr, err := q.k.GetFeederDelegation(ctx, valAddr)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryFeederDelegationResponse{FeederAddr: feederAddr.String()}, nil
}

// MissCounter queries oracle miss counter of a validator
func (q queryServer) MissCounter(ctx context.Context, req *types.QueryMissCounterRequest) (*types.QueryMissCounterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	missCounter, err := q.k.MissCounter.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryMissCounterResponse{MissCounter: missCounter}, nil
}

// AggregatePrevote queries an aggregate prevote of a validator
func (q queryServer) AggregatePrevote(ctx context.Context, req *types.QueryAggregatePrevoteRequest) (*types.QueryAggregatePrevoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	prevote, err := q.k.AggregateExchangeRatePrevote.Get(ctx, valAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, valAddr.String())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAggregatePrevoteResponse{AggregatePrevote: prevote}, nil
}

// AggregatePrevotes queries aggregate prevotes of all validators
func (q queryServer) AggregatePrevotes(ctx context.Context, req *types.QueryAggregatePrevotesRequest) (*types.QueryAggregatePrevotesResponse, error) {
	var prevotes []types.AggregateExchangeRatePrevote
	if err := q.k.AggregateExchangeRatePrevote.Walk(ctx, nil, func(_ sdk.ValAddress, prevote types.AggregateExchangeRatePrevote) (bool, error) {
		prevotes = append(prevotes, prevote)
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAggregatePrevotesResponse{AggregatePrevotes: prevotes}, nil
}

// AggregateVote queries an aggregate vote of a validator
func (q queryServer) AggregateVote(ctx context.Context, req *types.QueryAggregateVoteRequest) (*types.QueryAggregateVoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	vote, err := q.k.AggregateExchangeRateVote.Get(ctx, valAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, valAddr.String())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAggregateVoteResponse{AggregateVote: vote}, nil
}

// AggregateVotes queries aggregate votes of all validators
func (q queryServer) AggregateVotes(ctx context.Context, req *types.QueryAggregateVotesRequest) (*types.QueryAggregateVotesResponse, error) {
	var votes []types.AggregateExchangeRateVote
	if err := q.k.AggregateExchangeRateVote.Walk(ctx, nil, func(_ sdk.ValAddress, vote types.AggregateExchangeRateVote) (bool, error) {
		votes = append(votes, vote)
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAggregateVotesResponse{AggregateVotes: votes}, nil
}
