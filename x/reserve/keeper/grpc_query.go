package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

var _ types.QueryServer = (*queryServer)(nil)

type queryServer struct {
	k *Keeper
}

// NewQueryServerImpl returns the Reserve query server.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return &queryServer{k: k}
}

// Mandate queries the stored committee mandate and its term allowance.
func (q queryServer) Mandate(ctx context.Context, req *types.QueryMandateRequest) (*types.QueryMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	reserveMandate, err := q.k.Mandate.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Reserve mandate: %v", err)
	}
	used, err := q.k.AllowanceUsed.Get(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "getting Reserve allowance usage: %v", err)
	}
	remaining, err := reserveMandate.DeploymentAllowance.Amount.SafeSub(used)
	if err != nil || remaining.IsNegative() {
		return nil, status.Errorf(
			codes.Internal,
			"Reserve allowance usage %s exceeds mandate allowance %s",
			used,
			reserveMandate.DeploymentAllowance.Amount,
		)
	}

	return &types.QueryMandateResponse{
		Mandate:            reserveMandate,
		Active:             reserveMandate.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
		AllowanceUsed:      chain.NoahCoin(used),
		AllowanceRemaining: chain.NoahCoin(remaining),
	}, nil
}

// Position queries one permanent position record from wherever it lives, so a
// caller holding an identifier need not already know whether it has closed.
func (q queryServer) Position(ctx context.Context, req *types.QueryPositionRequest) (*types.QueryPositionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.PositionId == 0 {
		return nil, status.Error(codes.InvalidArgument, "position ID must be positive")
	}

	position, err := q.k.OpenPositions.Get(ctx, req.PositionId)
	if errors.Is(err, collections.ErrNotFound) {
		position, err = q.k.ClosedPositions.Get(ctx, req.PositionId)
	}
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "position %d not found", req.PositionId)
		}
		return nil, status.Errorf(codes.Internal, "getting position %d: %v", req.PositionId, err)
	}
	return &types.QueryPositionResponse{Position: position}, nil
}

// OpenPositions queries the paginated open set: what recognition credits and
// outstanding_deployed sums over. The page walks only the open store, so a
// counting page request totals the open set rather than the whole record.
func (q queryServer) OpenPositions(ctx context.Context, req *types.QueryOpenPositionsRequest) (*types.QueryOpenPositionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	// Key and offset together is the one page request the SDK accepts but
	// cannot answer coherently.
	if req.Pagination != nil && req.Pagination.Offset > 0 && len(req.Pagination.Key) != 0 {
		return nil, status.Error(codes.InvalidArgument, "pagination must not specify both key and offset")
	}

	positions, pageResponse, err := sdkquery.CollectionPaginate(
		ctx,
		q.k.OpenPositions,
		req.Pagination,
		func(_ uint64, position types.Position) (types.Position, error) {
			return position, nil
		},
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing open positions: %v", err)
	}
	return &types.QueryOpenPositionsResponse{Positions: positions, Pagination: pageResponse}, nil
}

// ClosedPositions queries the paginated closed record.
func (q queryServer) ClosedPositions(ctx context.Context, req *types.QueryClosedPositionsRequest) (*types.QueryClosedPositionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	// Key and offset together is the one page request the SDK accepts but
	// cannot answer coherently.
	if req.Pagination != nil && req.Pagination.Offset > 0 && len(req.Pagination.Key) != 0 {
		return nil, status.Error(codes.InvalidArgument, "pagination must not specify both key and offset")
	}

	positions, pageResponse, err := sdkquery.CollectionPaginate(
		ctx,
		q.k.ClosedPositions,
		req.Pagination,
		func(_ uint64, position types.Position) (types.Position, error) {
			return position, nil
		},
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing closed positions: %v", err)
	}
	return &types.QueryClosedPositionsResponse{Positions: positions, Pagination: pageResponse}, nil
}

// Ledger queries the paginated accounting ledger, optionally restricted to
// one position.
func (q queryServer) Ledger(ctx context.Context, req *types.QueryLedgerRequest) (*types.QueryLedgerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	// Key and offset together is the one page request the SDK accepts but
	// cannot answer coherently.
	if req.Pagination != nil && req.Pagination.Offset > 0 && len(req.Pagination.Key) != 0 {
		return nil, status.Error(codes.InvalidArgument, "pagination must not specify both key and offset")
	}

	// Filtering happens before pagination selects a page, so a filtered answer
	// is a page of that position's entries. The scan is over the whole ledger,
	// keyed by entry ID.
	entries, pageResponse, err := sdkquery.CollectionFilteredPaginate(
		ctx,
		q.k.Ledger,
		req.Pagination,
		func(_ uint64, entry types.AccountingEntry) (bool, error) {
			return req.PositionId == 0 || entry.PositionId == req.PositionId, nil
		},
		func(_ uint64, entry types.AccountingEntry) (types.AccountingEntry, error) {
			return entry, nil
		},
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing ledger: %v", err)
	}
	return &types.QueryLedgerResponse{Ledger: entries, Pagination: pageResponse}, nil
}

// Balance queries the Reserve's custody balance and outstanding deployment.
func (q queryServer) Balance(ctx context.Context, req *types.QueryBalanceRequest) (*types.QueryBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	outstanding := math.ZeroInt()
	if err := q.k.OpenPositions.Walk(ctx, nil, func(_ uint64, position types.Position) (bool, error) {
		var err error
		outstanding, err = outstanding.SafeAdd(position.Deployed.Amount)
		return false, err
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "summing outstanding deployment: %v", err)
	}

	return &types.QueryBalanceResponse{
		Balance:             chain.NoahCoin(q.k.balance(ctx)),
		OutstandingDeployed: chain.NoahCoin(outstanding),
	}, nil
}

// RecognitionPolicy queries the governance-owned asset eligibility set.
func (q queryServer) RecognitionPolicy(ctx context.Context, req *types.QueryRecognitionPolicyRequest) (*types.QueryRecognitionPolicyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	entries := make([]types.EligibilityEntry, 0)
	if err := q.k.RecognitionPolicy.Walk(ctx, nil, func(_ string, entry types.EligibilityEntry) (bool, error) {
		entries = append(entries, entry)
		return false, nil
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "iterating Reserve recognition policy: %v", err)
	}
	return &types.QueryRecognitionPolicyResponse{Entries: entries}, nil
}

// RecognisedCapital queries the figure Treasury sees and its decomposition.
func (q queryServer) RecognisedCapital(ctx context.Context, req *types.QueryRecognisedCapitalRequest) (*types.QueryRecognisedCapitalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	assets, err := q.k.AssetRecognitions(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "decomposing recognised capital: %v", err)
	}
	balance := q.k.balance(ctx)
	recognised, err := sumRecognised(balance, assets)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}

	return &types.QueryRecognisedCapitalResponse{
		Recognised:  chain.NoahCoin(recognised),
		NoahBalance: chain.NoahCoin(balance),
		Assets:      assets,
	}, nil
}
