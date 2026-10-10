package keeper

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/pagination"
	"github.com/ararat-network/ark/x/reserve/types"
)

var _ types.QueryServer = (*queryServer)(nil)

// queryServer validates each request itself and otherwise returns the keeper's error: a registered
// error carries its gRPC code, so no handler classifies.
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
		return nil, fmt.Errorf("getting Reserve mandate: %w", err)
	}
	used, err := q.k.AllowanceUsed.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Reserve allowance usage: %w", err)
	}
	remaining, err := reserveMandate.DeploymentAllowance.Amount.SafeSub(used)
	if err != nil || remaining.IsNegative() {
		return nil, fmt.Errorf("reserve allowance usage %s exceeds mandate allowance %s", used, reserveMandate.DeploymentAllowance.Amount)
	}

	return &types.QueryMandateResponse{
		Mandate:            reserveMandate,
		Active:             reserveMandate.IsActive(chain.BlockSeconds(ctx)),
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

	position, _, err := q.k.anyPosition(ctx, req.PositionId)
	if err != nil {
		return nil, err
	}
	return &types.QueryPositionResponse{Position: position}, nil
}

// OpenPositions queries the paginated open set: what recognition credits and
// outstanding_deployed sums over. The page walks only the open store.
func (q queryServer) OpenPositions(ctx context.Context, req *types.QueryOpenPositionsRequest) (*types.QueryOpenPositionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	page, err := pagination.Bounded(req.Pagination)
	if err != nil {
		return nil, err
	}

	positions, pageResponse, err := sdkquery.CollectionPaginate(
		ctx,
		q.k.OpenPositions,
		page,
		func(_ uint64, position types.Position) (types.Position, error) {
			return position, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("listing open positions: %w", err)
	}
	return &types.QueryOpenPositionsResponse{Positions: positions, Pagination: pageResponse}, nil
}

// ClosedPositions queries the paginated closed record.
func (q queryServer) ClosedPositions(ctx context.Context, req *types.QueryClosedPositionsRequest) (*types.QueryClosedPositionsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	page, err := pagination.Bounded(req.Pagination)
	if err != nil {
		return nil, err
	}

	positions, pageResponse, err := sdkquery.CollectionPaginate(
		ctx,
		q.k.ClosedPositions,
		page,
		func(_ uint64, position types.Position) (types.Position, error) {
			return position, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("listing closed positions: %w", err)
	}
	return &types.QueryClosedPositionsResponse{Positions: positions, Pagination: pageResponse}, nil
}

// Ledger queries the paginated accounting ledger, optionally restricted to
// one position.
func (q queryServer) Ledger(ctx context.Context, req *types.QueryLedgerRequest) (*types.QueryLedgerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	page, err := pagination.Bounded(req.Pagination)
	if err != nil {
		return nil, err
	}

	// A position's entries page through its index, so a page reads only that
	// position's entries; the whole ledger pages by entry ID. Either way the
	// next key belongs to the form of the request that returned it.
	var entries []types.AccountingEntry
	var pageResponse *sdkquery.PageResponse
	if req.PositionId == 0 {
		entries, pageResponse, err = sdkquery.CollectionPaginate(
			ctx,
			q.k.Ledger,
			page,
			func(_ uint64, entry types.AccountingEntry) (types.AccountingEntry, error) {
				return entry, nil
			},
		)
	} else {
		entries, pageResponse, err = sdkquery.CollectionPaginate(
			ctx,
			q.k.LedgerByPosition,
			page,
			func(key collections.Pair[uint64, uint64], _ collections.NoValue) (types.AccountingEntry, error) {
				return q.k.Ledger.Get(ctx, key.K2())
			},
			sdkquery.WithCollectionPaginationPairPrefix[uint64, uint64](req.PositionId),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("listing ledger: %w", err)
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
		return nil, fmt.Errorf("summing outstanding deployment: %w", err)
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
		return nil, fmt.Errorf("iterating Reserve recognition policy: %w", err)
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
		return nil, fmt.Errorf("decomposing recognised capital: %w", err)
	}
	balance := q.k.balance(ctx)
	recognised, err := sumRecognised(balance, assets)
	if err != nil {
		return nil, err
	}

	return &types.QueryRecognisedCapitalResponse{
		Recognised:  chain.NoahCoin(recognised),
		NoahBalance: chain.NoahCoin(balance),
		Assets:      assets,
	}, nil
}
