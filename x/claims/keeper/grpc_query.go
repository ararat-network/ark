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
	"github.com/ararat-network/ark/x/claims/types"
)

var _ types.QueryServer = (*queryServer)(nil)

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns the Claims query server.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Params queries the current Claims parameters.
func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Claims params: %v", err)
	}
	return &types.QueryParamsResponse{Params: params}, nil
}

// ClaimsMandate queries the stored committee mandate and its term allowance.
func (q queryServer) ClaimsMandate(ctx context.Context, req *types.QueryClaimsMandateRequest) (*types.QueryClaimsMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	claimsMandate, err := q.k.ClaimsMandate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Claims mandate: %v", err)
	}
	allowanceUsed, err := q.k.ClaimsAllowanceUsed.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Claims allowance usage: %v", err)
	}
	allowanceRemaining, err := claimsMandate.CommitteeClaimLimit.Amount.SafeSub(allowanceUsed)
	if err != nil || allowanceRemaining.IsNegative() {
		return nil, status.Errorf(
			codes.Internal,
			"Claims allowance usage %s exceeds mandate limit %s",
			allowanceUsed,
			claimsMandate.CommitteeClaimLimit,
		)
	}

	return &types.QueryClaimsMandateResponse{
		Mandate:            claimsMandate,
		Active:             claimsMandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
		AllowanceUsed:      chain.NoahCoin(allowanceUsed),
		AllowanceRemaining: chain.NoahCoin(allowanceRemaining),
	}, nil
}

// Balance reports custody and reservations across all terms. Mandate allowance is separate and
// resets on replacement; pending reservations survive it.
func (q queryServer) Balance(ctx context.Context, req *types.QueryBalanceRequest) (*types.QueryBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	reserved, err := q.k.InsuranceReserved.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Insurance reservation: %v", err)
	}

	return &types.QueryBalanceResponse{
		Balance:  chain.NoahCoin(q.k.balance(ctx)),
		Reserved: chain.NoahCoin(reserved),
	}, nil
}

// Claim queries one permanent claim record.
func (q queryServer) Claim(ctx context.Context, req *types.QueryClaimRequest) (*types.QueryClaimResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.ClaimId == 0 {
		return nil, status.Error(codes.InvalidArgument, "claim ID must be positive")
	}

	claim, err := q.k.Claims.Get(ctx, req.ClaimId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "claim %d not found", req.ClaimId)
		}
		return nil, status.Errorf(codes.Internal, "getting claim %d: %v", req.ClaimId, err)
	}
	return &types.QueryClaimResponse{Claim: claim}, nil
}

// Claims queries the paginated permanent claim record.
func (q queryServer) Claims(ctx context.Context, req *types.QueryClaimsRequest) (*types.QueryClaimsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Pagination != nil && req.Pagination.Offset > 0 && len(req.Pagination.Key) != 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"pagination must not specify both key and offset",
		)
	}

	claims, pageResponse, err := sdkquery.CollectionPaginate(
		ctx,
		q.k.Claims,
		req.Pagination,
		func(_ uint64, claim types.Claim) (types.Claim, error) {
			return claim, nil
		},
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing claims: %v", err)
	}

	return &types.QueryClaimsResponse{
		Claims:     claims,
		Pagination: pageResponse,
	}, nil
}
