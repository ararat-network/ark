package keeper

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/security/types"
)

var _ types.QueryServer = queryServer{}

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns an implementation of the security QueryServer
// interface for the provided Keeper.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{k: k}
}

// SecurityMandate returns the committee appointment and whether it may act at
// the current height.
func (q queryServer) SecurityMandate(ctx context.Context, req *types.QuerySecurityMandateRequest) (*types.QuerySecurityMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	securityMandate, err := q.k.Mandate.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("getting security mandate: %s", err))
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	return &types.QuerySecurityMandateResponse{
		Mandate: securityMandate,
		Active:  securityMandate.IsActive(uint64(sdkCtx.BlockHeight())),
	}, nil
}

// CommitteePlan returns the recorded plan and whether it still matches x/upgrade's pending plan. A
// stale record grants no committee authority.
func (q queryServer) CommitteePlan(ctx context.Context, req *types.QueryCommitteePlanRequest) (*types.QueryCommitteePlanResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	record, err := q.k.CommitteePlan.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("getting committee plan: %s", err))
	}

	matchesPending := false
	if !record.IsZero() {
		pending, hasPending, err := q.k.pendingUpgradePlan(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		matchesPending = hasPending && record.Matches(pending.Name, pending.Height)
	}

	return &types.QueryCommitteePlanResponse{
		CommitteePlan:  record,
		MatchesPending: matchesPending,
	}, nil
}
