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
}

func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Params queries params of distribution module
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury params: %v", err)
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

// TaxRate return the current tax rate
func (q queryServer) TaxRate(ctx context.Context, req *types.QueryTaxRateRequest) (*types.QueryTaxRateResponse, error) {
	taxRate, err := q.k.TaxRate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury tax rate: %v", err)
	}
	return &types.QueryTaxRateResponse{TaxRate: taxRate}, nil
}

// TaxCap returns the tax cap of a denom
func (q queryServer) TaxCap(ctx context.Context, req *types.QueryTaxCapRequest) (*types.QueryTaxCapResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid denom %q: %v", req.Denom, err)
	}

	taxCap, err := q.k.TaxCaps.Get(ctx, req.Denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "tax cap not found for denom %s", req.Denom)
		}
		return nil, status.Errorf(codes.Internal, "getting tax cap for denom %s: %v", req.Denom, err)
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
		return nil, status.Errorf(codes.Internal, "listing treasury tax caps: %v", err)
	}

	return &types.QueryTaxCapsResponse{TaxCaps: taxCaps}, nil
}

// RewardWeight return the current reward weight
func (q queryServer) RewardWeight(ctx context.Context, req *types.QueryRewardWeightRequest) (*types.QueryRewardWeightResponse, error) {
	rewardWeight, err := q.k.RewardWeight.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury reward weight: %v", err)
	}
	return &types.QueryRewardWeightResponse{RewardWeight: rewardWeight}, nil
}

// SeigniorageProceeds return the current seigniorage proceeds
func (q queryServer) SeigniorageProceeds(ctx context.Context, req *types.QuerySeigniorageProceedsRequest) (*types.QuerySeigniorageProceedsResponse, error) {
	epochSeiniorage, err := q.k.ComputeEpochSeigniorage(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "computing treasury seigniorage proceeds: %v", err)
	}
	return &types.QuerySeigniorageProceedsResponse{SeigniorageProceeds: epochSeiniorage}, nil
}

// TaxProceeds return the current tax proceeds
func (q queryServer) TaxProceeds(ctx context.Context, req *types.QueryTaxProceedsRequest) (*types.QueryTaxProceedsResponse, error) {
	epochTaxProceeds, err := q.k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury tax proceeds: %v", err)
	}
	return &types.QueryTaxProceedsResponse{TaxProceeds: epochTaxProceeds.TaxProceeds}, nil
}

// Indicators returns year and month rolling averages of tax rewards per staked Ark.
// The query blends finalised epoch indicators with the current in-progress epoch.
func (q queryServer) Indicators(ctx context.Context, req *types.QueryIndicatorsRequest) (*types.QueryIndicatorsResponse, error) {
	totalStakedArk, err := q.k.stakingKeeper.TotalValidatorPower(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting total staked ark: %v", err)
	} else if totalStakedArk.IsZero() {
		return &types.QueryIndicatorsResponse{
			TRAYear:  math.LegacyZeroDec(),
			TRAMonth: math.LegacyZeroDec(),
		}, nil
	}

	epochTaxProceeds, err := q.k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting treasury tax proceeds: %v", err)
	}
	taxProceeds := sdk.NewDecCoinsFromCoins(epochTaxProceeds.TaxProceeds...)
	taxRewards := q.k.alignCoins(ctx, taxProceeds, core.MicroSDRDenom)

	epoch := q.k.GetEpoch(ctx)
	var res types.QueryIndicatorsResponse
	if epoch == 0 {
		res = types.QueryIndicatorsResponse{
			TRAYear:  taxRewards.QuoInt(totalStakedArk),
			TRAMonth: taxRewards.QuoInt(totalStakedArk),
		}
	} else {
		params, err := q.k.Params.Get(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "getting treasury params: %v", err)
		}
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		previousEpochCtx := sdkCtx.WithBlockHeight(sdkCtx.BlockHeight() - int64(core.BlocksPerWeek))
		traYear, err := q.k.rollingAverageIndicator(previousEpochCtx, params.WindowLong-1)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "computing yearly treasury indicator average: %v", err)
		}
		traMonth, err := q.k.rollingAverageIndicator(previousEpochCtx, params.WindowShort-1)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "computing monthly treasury indicator average: %v", err)
		}

		computedEpochForYear := int64(math.Min(float64(params.WindowLong-1), float64(epoch)))
		computedEpochForMonth := int64(math.Min(float64(params.WindowShort-1), float64(epoch)))

		traYear = traYear.MulInt64(computedEpochForYear).Add(taxRewards.QuoInt(totalStakedArk)).QuoInt64(computedEpochForYear + 1)
		traMonth = traMonth.MulInt64(computedEpochForMonth).Add(taxRewards.QuoInt(totalStakedArk)).QuoInt64(computedEpochForMonth + 1)

		res = types.QueryIndicatorsResponse{
			TRAYear:  traYear,
			TRAMonth: traMonth,
		}
	}

	return &res, nil
}
