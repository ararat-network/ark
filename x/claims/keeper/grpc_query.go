package keeper

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

var _ types.QueryServer = (*queryServer)(nil)

// queryServer validates each request itself and otherwise returns the keeper's error: a registered
// error carries its gRPC code, so no handler classifies.
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
		return nil, fmt.Errorf("getting Claims params: %w", err)
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
		return nil, fmt.Errorf("getting Claims mandate: %w", err)
	}
	allowanceUsed, err := q.k.ClaimsAllowanceUsed.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims allowance usage: %w", err)
	}
	allowanceRemaining, err := claimsMandate.CommitteeClaimLimit.Amount.SafeSub(allowanceUsed)
	if err != nil || allowanceRemaining.IsNegative() {
		return nil, fmt.Errorf("claims allowance usage %s exceeds mandate limit %s", allowanceUsed, claimsMandate.CommitteeClaimLimit)
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
		return nil, fmt.Errorf("getting Insurance reservation: %w", err)
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

	claim, err := q.k.getClaim(ctx, req.ClaimId)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("listing claims: %w", err)
	}

	return &types.QueryClaimsResponse{
		Claims:     claims,
		Pagination: pageResponse,
	}, nil
}
