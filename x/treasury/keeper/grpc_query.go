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
	"noah/x/treasury/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k *Keeper
	types.UnimplementedQueryServer
}

func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{k: k}
}

// Params queries params of distribution module
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

// TaxRate return the current tax rate
func (q queryServer) TaxRate(ctx context.Context, req *types.QueryTaxRateRequest) (*types.QueryTaxRateResponse, error) {
	taxRate, err := q.k.TaxRate.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryTaxRateResponse{TaxRate: taxRate}, nil
}

// TaxCap returns the tax cap of a denom
func (q queryServer) TaxCap(ctx context.Context, req *types.QueryTaxCapRequest) (*types.QueryTaxCapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid denom")
	}

	taxCap, err := q.k.TaxCaps.Get(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "tax cap not found for denom %s", req.Denom)
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryTaxCapResponse{TaxCap: taxCap}, nil
}

// TaxCaps returns the all tax caps
func (q queryServer) TaxCaps(ctx context.Context, req *types.QueryTaxCapsRequest) (*types.QueryTaxCapsResponse, error) {
	var taxCaps []types.TaxCap
	err := q.k.TaxCaps.Walk(ctx, nil, func(denom string, taxCap math.Int) (bool, error) {
		taxCaps = append(taxCaps, types.TaxCap{
			Denom:  denom,
			TaxCap: taxCap,
		})
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryTaxCapsResponse{TaxCaps: taxCaps}, nil
}

// RewardWeight return the current reward weight
func (q queryServer) RewardWeight(ctx context.Context, req *types.QueryRewardWeightRequest) (*types.QueryRewardWeightResponse, error) {
	rewardWeight, err := q.k.RewardWeight.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRewardWeightResponse{RewardWeight: rewardWeight}, nil
}

// SeigniorageProceeds return the current seigniorage proceeds
func (q queryServer) SeigniorageProceeds(ctx context.Context, req *types.QuerySeigniorageProceedsRequest) (*types.QuerySeigniorageProceedsResponse, error) {
	epochSeiniorage, err := q.k.ComputeEpochSeigniorage(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QuerySeigniorageProceedsResponse{SeigniorageProceeds: epochSeiniorage}, nil
}

// TaxProceeds return the current tax proceeds
func (q queryServer) TaxProceeds(ctx context.Context, req *types.QueryTaxProceedsRequest) (*types.QueryTaxProceedsResponse, error) {
	epochTaxProceeds, err := q.k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryTaxProceedsResponse{TaxProceeds: epochTaxProceeds.TaxProceeds}, nil
}

// Indicators return the current trl informations
func (q queryServer) Indicators(ctx context.Context, req *types.QueryIndicatorsRequest) (*types.QueryIndicatorsResponse, error) {
	// Compute Total Staked Ark (TSA)
	TSA := q.k.stakingKeeper.TotalBondedTokens(ctx)

	// Compute Tax Rewards (TR)
	epochTaxProceeds, err := q.k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	taxRewards := sdk.NewDecCoinsFromCoins(epochTaxProceeds.TaxProceeds...)
	TR := q.k.alignCoins(ctx, taxRewards, core.MicroSDRDenom)

	epoch := q.k.GetEpoch(ctx)
	var res types.QueryIndicatorsResponse
	if epoch == 0 {
		if TSA.IsZero() {
			res = types.QueryIndicatorsResponse{
				TRAYear:  math.LegacyZeroDec(),
				TRAMonth: math.LegacyZeroDec(),
			}
		} else {
			res = types.QueryIndicatorsResponse{
				TRAYear:  TR.QuoInt(TSA),
				TRAMonth: TR.QuoInt(TSA),
			}
		}
	} else {
		params, err := q.k.Params.Get(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		previousEpochCtx := sdkCtx.WithBlockHeight(sdkCtx.BlockHeight() - int64(core.BlocksPerWeek))
		traYear, err := q.k.rollingAverageIndicator(previousEpochCtx, params.WindowLong-1)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		traMonth, err := q.k.rollingAverageIndicator(previousEpochCtx, params.WindowShort-1)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		computedEpochForYear := int64(math.Min(float64(params.WindowLong-1), float64(epoch)))
		computedEpochForMonth := int64(math.Min(float64(params.WindowShort-1), float64(epoch)))

		traYear = traYear.MulInt64(computedEpochForYear).Add(TR.QuoInt(TSA)).QuoInt64(computedEpochForYear + 1)
		traMonth = traMonth.MulInt64(computedEpochForMonth).Add(TR.QuoInt(TSA)).QuoInt64(computedEpochForMonth + 1)

		res = types.QueryIndicatorsResponse{
			TRAYear:  traYear,
			TRAMonth: traMonth,
		}
	}

	return &res, nil
}
