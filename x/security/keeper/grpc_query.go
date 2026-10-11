package keeper

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/security/types"
)

var _ types.QueryServer = queryServer{}

// queryServer validates each request itself and otherwise returns the keeper's error. Security
// registers no errors: nothing branches on one of its failures.
type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns an implementation of the security QueryServer
// interface for the provided Keeper.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{k: k}
}

// SecurityMandate returns the committee appointment and whether it may act at
// the current block time.
func (q queryServer) SecurityMandate(ctx context.Context, req *types.QuerySecurityMandateRequest) (*types.QuerySecurityMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	securityMandate, err := q.k.Mandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting security mandate: %w", err)
	}
	return &types.QuerySecurityMandateResponse{
		Mandate: securityMandate,
		Active:  securityMandate.IsActive(chain.BlockSeconds(ctx)),
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
		return nil, fmt.Errorf("getting committee plan: %w", err)
	}

	matchesPending := false
	if !record.IsZero() {
		pending, hasPending, err := q.k.pendingUpgradePlan(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading the pending upgrade plan: %w", err)
		}
		matchesPending = hasPending && record.Matches(pending.Name, pending.Height)
	}

	return &types.QueryCommitteePlanResponse{
		CommitteePlan:  record,
		MatchesPending: matchesPending,
	}, nil
}
