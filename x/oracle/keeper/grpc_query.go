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

	tobinTax, err := q.k.TobinTax.Get(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "tobin tax not found for denom %s", req.Denom)
		}
		return nil, status.Errorf(codes.Internal, "getting tobin tax for denom %s: %v", req.Denom, err)
	}

	return &types.QueryTobinTaxResponse{TobinTax: tobinTax}, nil
}

// TobinTaxes queries tobin taxes of all denoms
func (q queryServer) TobinTaxes(ctx context.Context, req *types.QueryTobinTaxesRequest) (*types.QueryTobinTaxesResponse, error) {
	var tobinTaxes types.TobinTaxes
	if err := q.k.TobinTax.Walk(ctx, nil, func(denom string, rate math.LegacyDec) (bool, error) {
		tobinTaxes = append(tobinTaxes, types.TobinTax{
			Denom:    denom,
			TobinTax: rate,
		})
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing oracle tobin taxes: %v", err)
	}

	return &types.QueryTobinTaxesResponse{TobinTaxes: tobinTaxes}, nil
}

// Actives queries all denoms for which exchange rates exist
func (q queryServer) Actives(ctx context.Context, req *types.QueryActivesRequest) (*types.QueryActivesResponse, error) {
	var actives []string
	if err := q.k.ExchangeRate.Walk(ctx, nil, func(denom string, exchangeRate types.ExchangeRate) (bool, error) {
		actives = append(actives, denom)
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing active oracle denoms: %v", err)
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
		return nil, status.Errorf(codes.Internal, "listing oracle vote targets: %v", err)
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
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address %q: %v", req.ValidatorAddr, err)
	}

	feederAddr, err := q.k.GetFeederDelegation(ctx, valAddr)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting feeder delegation for validator %s: %v", valAddr, err)
	}
	return &types.QueryFeederDelegationResponse{FeederAddr: feederAddr.String()}, nil
}

// MissCount queries oracle miss counter of a validator
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

// Prevote queries an aggregate prevote of a validator
func (q queryServer) Prevote(ctx context.Context, req *types.QueryPrevoteRequest) (*types.QueryPrevoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address %q: %v", req.ValidatorAddr, err)
	}
	prevote, err := q.k.Prevote.Get(ctx, valAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "prevote not found for validator %s", valAddr)
		}
		return nil, status.Errorf(codes.Internal, "getting prevote for validator %s: %v", valAddr, err)
	}

	return &types.QueryPrevoteResponse{Prevote: prevote}, nil
}

// Prevotes queries aggregate prevotes of all validators
func (q queryServer) Prevotes(ctx context.Context, req *types.QueryPrevotesRequest) (*types.QueryPrevotesResponse, error) {
	var prevotes []types.Prevote
	if err := q.k.Prevote.Walk(ctx, nil, func(_ sdk.ValAddress, prevote types.Prevote) (bool, error) {
		prevotes = append(prevotes, prevote)
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing oracle prevotes: %v", err)
	}

	return &types.QueryPrevotesResponse{Prevotes: prevotes}, nil
}

// Vote queries an aggregate vote of a validator
func (q queryServer) Vote(ctx context.Context, req *types.QueryVoteRequest) (*types.QueryVoteResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address %q: %v", req.ValidatorAddr, err)
	}
	vote, err := q.k.Vote.Get(ctx, valAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "vote not found for validator %s", valAddr)
		}
		return nil, status.Errorf(codes.Internal, "getting vote for validator %s: %v", valAddr, err)
	}

	return &types.QueryVoteResponse{Vote: vote}, nil
}

// Votes queries aggregate votes of all validators
func (q queryServer) Votes(ctx context.Context, req *types.QueryVotesRequest) (*types.QueryVotesResponse, error) {
	var votes []types.Vote
	if err := q.k.Vote.Walk(ctx, nil, func(_ sdk.ValAddress, vote types.Vote) (bool, error) {
		votes = append(votes, vote)
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "listing oracle votes: %v", err)
	}

	return &types.QueryVotesResponse{Votes: votes}, nil
}
