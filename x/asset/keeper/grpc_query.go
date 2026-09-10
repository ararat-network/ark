package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
)

var _ types.QueryServer = (*queryServer)(nil)

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns the Asset query server.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Params returns the asset module parameters.
func (q queryServer) Params(
	ctx context.Context,
	_ *types.QueryParamsRequest,
) (*types.QueryParamsResponse, error) {
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting asset params: %v", err)
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

// Asset returns one registered asset.
func (q queryServer) Asset(ctx context.Context, req *types.QueryAssetRequest) (*types.QueryAssetResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := chain.ValidatePricedDenom(req.Denom); err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"invalid asset denom %q: %v",
			req.Denom,
			err,
		)
	}

	pricings, err := q.k.Pricings(ctx, req.Denom)
	if err != nil {
		return nil, status.Errorf(
			codes.Internal,
			"pricing asset %s: %v",
			req.Denom,
			err,
		)
	}

	// A verdict without a record is a denomination the registry does not list:
	// the fold reports that as an unpriced verdict rather than an error, and
	// answering a lookup for one asset is where it becomes a not-found.
	priced := pricings[req.Denom]
	if priced.Asset.Denom == "" {
		return nil, status.Errorf(
			codes.NotFound,
			"asset %s not found",
			req.Denom,
		)
	}

	return &types.QueryAssetResponse{PricedAsset: priced}, nil
}

// Assets returns all registry entries with pricing verdicts. An unavailable feed makes only its
// member unpriced; store or structural faults fail the query.
func (q queryServer) Assets(ctx context.Context, req *types.QueryAssetsRequest) (*types.QueryAssetsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	denoms, pricings, err := q.k.PricedAssets(ctx)
	if err != nil {
		return nil, status.Errorf(
			codes.Internal,
			"pricing assets: %v",
			err,
		)
	}

	priced := make([]types.PricedAsset, len(denoms))
	for i, denom := range denoms {
		priced[i] = pricings[denom]
	}

	return &types.QueryAssetsResponse{PricedAssets: priced}, nil
}

// SettlementPlan returns the active settlement plan for one asset.
func (q queryServer) SettlementPlan(ctx context.Context, req *types.QuerySettlementPlanRequest) (*types.QuerySettlementPlanResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := chain.ValidatePricedDenom(req.Denom); err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"invalid asset denom %q: %v",
			req.Denom,
			err,
		)
	}

	plan, found, err := q.k.GetSettlementPlan(ctx, req.Denom)
	if err != nil {
		return nil, status.Errorf(
			codes.Internal,
			"getting settlement plan for asset %s: %v",
			req.Denom,
			err,
		)
	}
	if !found {
		return nil, status.Errorf(
			codes.NotFound,
			"settlement plan for asset %s not found",
			req.Denom,
		)
	}

	return &types.QuerySettlementPlanResponse{SettlementPlan: plan}, nil
}

// ResolutionHistory returns one asset's append-only write-off records in
// increasing version order.
func (q queryServer) ResolutionHistory(ctx context.Context, req *types.QueryResolutionHistoryRequest) (*types.QueryResolutionHistoryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := chain.ValidatePricedDenom(req.Denom); err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			"invalid asset denom %q: %v",
			req.Denom,
			err,
		)
	}
	if req.Pagination != nil {
		if req.Pagination.Offset > 0 && len(req.Pagination.Key) != 0 {
			return nil, status.Error(
				codes.InvalidArgument,
				"pagination must not specify both key and offset",
			)
		}
		if req.Pagination.Reverse {
			return nil, status.Error(
				codes.InvalidArgument,
				"reverse pagination is not supported",
			)
		}
	}
	if _, err := q.k.GetAsset(ctx, req.Denom); err != nil {
		if errors.Is(err, types.ErrAssetNotFound) {
			return nil, status.Errorf(
				codes.NotFound,
				"asset %s not found",
				req.Denom,
			)
		}
		return nil, status.Errorf(
			codes.Internal,
			"getting asset %s: %v",
			req.Denom,
			err,
		)
	}

	records, pageResponse, err := sdkquery.CollectionPaginate(
		ctx,
		q.k.ResolutionRecords,
		req.Pagination,
		func(
			_ collections.Pair[string, uint64],
			record types.ResolutionRecord,
		) (types.ResolutionRecord, error) {
			return record, nil
		},
		sdkquery.WithCollectionPaginationPairPrefix[string, uint64](req.Denom),
	)
	if err != nil {
		return nil, status.Errorf(
			codes.Internal,
			"listing write-off history for asset %s: %v",
			req.Denom,
			err,
		)
	}

	return &types.QueryResolutionHistoryResponse{
		ResolutionRecords: records,
		Pagination:        pageResponse,
	}, nil
}

// EmergencyMandate returns the committee appointment, its current effective
// status, and the suspensions already consumed under its term.
func (q queryServer) EmergencyMandate(ctx context.Context, req *types.QueryEmergencyMandateRequest) (*types.QueryEmergencyMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	emergencyMandate, err := q.k.EmergencyMandate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(
			codes.Internal,
			"getting emergency mandate: %v",
			err,
		)
	}

	suspendedDenoms := []string{}
	if err := q.k.EmergencySuspensions.Walk(
		ctx,
		nil,
		func(denom string) (bool, error) {
			suspendedDenoms = append(suspendedDenoms, denom)
			return false, nil
		},
	); err != nil {
		return nil, status.Errorf(
			codes.Internal,
			"listing consumed emergency suspensions: %v",
			err,
		)
	}

	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()

	return &types.QueryEmergencyMandateResponse{
		Mandate:         emergencyMandate,
		Active:          emergencyMandate.IsActive(uint64(blockHeight)),
		SuspendedDenoms: suspendedDenoms,
	}, nil
}
