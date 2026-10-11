// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/oracle/keeper/querier.go.
// Modified for Ark: modern query handlers and chain-specific state.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/decimal"
	"github.com/ararat-network/ark/x/oracle/types"
)

var _ types.QueryServer = (*queryServer)(nil)

// queryServer validates each request itself and otherwise returns the keeper's error: a registered
// error carries its gRPC code, so no handler classifies.
type queryServer struct {
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
		return nil, fmt.Errorf("getting oracle params: %w", err)
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
		return nil, fmt.Errorf("getting exchange rate for %s: %w", req.Denom, err)
	}

	return &types.QueryExchangeRateResponse{
		ExchangeRate: exchangeRate,
		UnitsPerNoah: unitsPerNoah(exchangeRate),
	}, nil
}

// ExchangeRates queries every fresh exchange rate, in denomination order and
// in both readings.
func (q queryServer) ExchangeRates(ctx context.Context, req *types.QueryExchangeRatesRequest) (*types.QueryExchangeRatesResponse, error) {
	exchangeRates, err := q.k.GetExchangeRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting oracle exchange rates: %w", err)
	}

	readings := make([]types.ExchangeRateReading, 0, len(exchangeRates))
	for _, exchangeRate := range exchangeRates {
		readings = append(readings, types.ExchangeRateReading{
			Denom:        exchangeRate.Denom,
			ExchangeRate: exchangeRate.Amount,
			UnitsPerNoah: unitsPerNoah(exchangeRate.Amount),
		})
	}

	return &types.QueryExchangeRatesResponse{ExchangeRates: readings}, nil
}

// StoredRates queries every stored rate whatever its age, in denomination
// order, with the block that set it.
func (q queryServer) StoredRates(ctx context.Context, req *types.QueryStoredRatesRequest) (*types.QueryStoredRatesResponse, error) {
	var rates []types.ExchangeRate
	if err := q.k.ExchangeRate.Walk(ctx, nil, func(_ string, rate types.ExchangeRate) (bool, error) {
		rates = append(rates, rate)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating oracle exchange rates: %w", err)
	}

	return &types.QueryStoredRatesResponse{Rates: rates}, nil
}

// unitsPerNoah is a query-only reciprocal of the stored rate. Values below decimal precision
// display as zero without invalidating the stored-rate response.
func unitsPerNoah(rate math.LegacyDec) math.LegacyDec {
	if rate.IsNil() || !rate.IsPositive() {
		return math.LegacyZeroDec()
	}
	reciprocal, err := decimal.Quo(math.LegacyOneDec(), rate)
	if err != nil {
		return math.LegacyZeroDec()
	}
	return reciprocal
}

// Feeds queries the active feed set and any scheduled transitions.
func (q queryServer) Feeds(ctx context.Context, req *types.QueryFeedsRequest) (*types.QueryFeedsResponse, error) {
	feeds, err := q.k.Feeds.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting oracle feeds: %w", err)
	}

	return &types.QueryFeedsResponse{Feeds: feeds}, nil
}

// FeedReferents queries the consumer claims pinning a feed. Existence and the
// claims themselves both come from the collector MsgRemoveFeed consults, so a
// denom this reports as unremovable is exactly one governance would reject.
func (q queryServer) FeedReferents(ctx context.Context, req *types.QueryFeedReferentsRequest) (*types.QueryFeedReferentsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid denom %q: %v", req.Denom, err)
	}

	referents, err := q.k.feedReferents(ctx, req.Denom)
	if err != nil {
		return nil, fmt.Errorf("getting feed referents for %s: %w", req.Denom, err)
	}

	return &types.QueryFeedReferentsResponse{Referents: referents}, nil
}

// ReferenceDenom queries the denomination whose feed is the protocol reference.
func (q queryServer) ReferenceDenom(ctx context.Context, _ *types.QueryReferenceDenomRequest) (*types.QueryReferenceDenomResponse, error) {
	referenceDenom, err := q.k.GetReferenceDenom(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting protocol reference: %w", err)
	}

	return &types.QueryReferenceDenomResponse{ReferenceDenom: referenceDenom}, nil
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
			return nil, fmt.Errorf("getting reward weight for validator %s: %w", valAddr, err)
		}
		rewardWeight = math.ZeroInt()
	}

	return &types.QueryRewardWeightResponse{RewardWeight: rewardWeight}, nil
}

// Attendance queries a validator's in-progress oracle attendance counters.
func (q queryServer) Attendance(ctx context.Context, req *types.QueryAttendanceRequest) (*types.QueryAttendanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	valAddr, err := sdk.ValAddressFromBech32(req.ValidatorAddr)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid validator address %q: %v", req.ValidatorAddr, err)
	}
	attendance, err := q.k.Attendance.Get(ctx, valAddr)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("getting attendance for validator %s: %w", valAddr, err)
	}

	return &types.QueryAttendanceResponse{Attendance: attendance}, nil
}
