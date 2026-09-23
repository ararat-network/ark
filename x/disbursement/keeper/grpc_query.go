package keeper

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	query "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

type queryServer struct {
	types.UnimplementedQueryServer
	k *Keeper
}

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl exposes indexed grant queries.
func NewQueryServerImpl(k *Keeper) types.QueryServer { return queryServer{k: k} }

func queryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, collections.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

func page(p *query.PageRequest) (*query.PageRequest, error) {
	if p == nil {
		return &query.PageRequest{Limit: types.MaxPageSize}, nil
	}
	if p.Offset != 0 || p.CountTotal || p.Limit > types.MaxPageSize {
		return nil, status.Error(codes.InvalidArgument, "use key pagination, without total counting, with a limit at most 100")
	}
	copied := *p
	if copied.Limit == 0 {
		copied.Limit = types.MaxPageSize
	}
	return &copied, nil
}

func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	p, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, queryError(err)
	}
	policy, err := q.k.OwnershipPolicy.Get(ctx)
	if err != nil {
		return nil, queryError(err)
	}
	founding, err := q.k.FoundingStake.Get(ctx)
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryParamsResponse{Params: p, OwnershipPolicy: policy, FoundingStake: founding}, nil
}

func (q queryServer) Grant(ctx context.Context, req *types.QueryGrantRequest) (*types.QueryGrantResponse, error) {
	if req == nil || req.Id == 0 {
		return nil, status.Error(codes.InvalidArgument, "grant ID required")
	}
	g, err := q.k.Grants.Get(ctx, req.Id)
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryGrantResponse{Grant: g}, nil
}

func (q queryServer) Grants(ctx context.Context, req *types.QueryGrantsRequest) (*types.QueryGrantsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	p, err := page(req.Pagination)
	if err != nil {
		return nil, err
	}
	var grants []types.Grant
	var response *query.PageResponse
	if req.Beneficiary == "" {
		grants, response, err = query.CollectionPaginate(ctx, q.k.Grants, p, func(_ uint64, g types.Grant) (types.Grant, error) { return g, nil })
	} else {
		a, parseErr := chain.ParseCanonicalAccountAddress("beneficiary", req.Beneficiary)
		if parseErr != nil {
			return nil, status.Error(codes.InvalidArgument, parseErr.Error())
		}
		grants, response, err = query.CollectionPaginate(ctx, q.k.GrantsByBeneficiary, p, func(key collections.Pair[sdk.AccAddress, uint64], _ bool) (types.Grant, error) {
			return q.k.Grants.Get(ctx, key.K2())
		}, query.WithCollectionPaginationPairPrefix[sdk.AccAddress, uint64](a))
	}
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryGrantsResponse{Grants: grants, Pagination: response}, nil
}

func (q queryServer) Member(ctx context.Context, req *types.QueryMemberRequest) (*types.QueryMemberResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	a, err := chain.ParseCanonicalAccountAddress("member", req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	id, err := q.k.Members.Get(ctx, a)
	if err != nil {
		return nil, queryError(err)
	}
	g, err := q.k.Grants.Get(ctx, id)
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryMemberResponse{Grant: g}, nil
}

func (q queryServer) Beneficiary(ctx context.Context, req *types.QueryBeneficiaryRequest) (*types.QueryBeneficiaryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	a, err := chain.ParseCanonicalAccountAddress("beneficiary", req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := q.k.Beneficiaries.Get(ctx, a)
	if err != nil {
		return nil, queryError(err)
	}
	f, err := q.k.founder(ctx, a)
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryBeneficiaryResponse{Beneficiary: p, Founder: f}, nil
}

func (q queryServer) Releasable(ctx context.Context, req *types.QueryReleasableRequest) (*types.QueryReleasableResponse, error) {
	if req == nil || req.Id == 0 {
		return nil, status.Error(codes.InvalidArgument, "grant ID required")
	}
	g, err := q.k.Grants.Get(ctx, req.Id)
	if err != nil {
		return nil, queryError(err)
	}
	r, err := q.k.releasable(ctx, g)
	return r, queryError(err)
}

func (q queryServer) Balance(ctx context.Context, req *types.QueryBalanceRequest) (*types.QueryBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	if err := sdk.ValidateDenom(req.Denom); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	t, err := q.k.totals(ctx, req.Denom)
	if err != nil {
		return nil, queryError(err)
	}
	available, err := q.k.unallocated(ctx, req.Denom)
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryBalanceResponse{Balance: q.k.bank.GetBalance(ctx, q.k.address, req.Denom), Totals: t, Unallocated: available}, nil
}

func (q queryServer) Totals(ctx context.Context, req *types.QueryTotalsRequest) (*types.QueryTotalsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	p, err := page(req.Pagination)
	if err != nil {
		return nil, err
	}
	totals, response, err := query.CollectionPaginate(ctx, q.k.Totals, p, func(_ string, t types.DenomTotals) (types.DenomTotals, error) { return t, nil })
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryTotalsResponse{Totals: totals, Pagination: response}, nil
}

func (q queryServer) Journal(ctx context.Context, req *types.QueryJournalRequest) (*types.QueryJournalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	p, err := page(req.Pagination)
	if err != nil {
		return nil, err
	}
	var entries []types.JournalEntry
	var response *query.PageResponse
	if req.GrantId == 0 {
		entries, response, err = query.CollectionPaginate(ctx, q.k.Journal, p, func(_ uint64, e types.JournalEntry) (types.JournalEntry, error) { return e, nil })
	} else {
		entries, response, err = query.CollectionPaginate(ctx, q.k.JournalByGrant, p, func(key collections.Pair[uint64, uint64], _ bool) (types.JournalEntry, error) {
			return q.k.Journal.Get(ctx, key.K2())
		}, query.WithCollectionPaginationPairPrefix[uint64, uint64](req.GrantId))
	}
	if err != nil {
		return nil, queryError(err)
	}
	return &types.QueryJournalResponse{Entries: entries, Pagination: response}, nil
}

func (q queryServer) Issuance(ctx context.Context, req *types.QueryIssuanceRequest) (*types.QueryIssuanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "nil request")
	}
	p, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, queryError(err)
	}
	log, used, err := q.k.issuanceWindow(ctx, p)
	if err != nil {
		return nil, queryError(err)
	}
	remaining := uint64(0)
	if used < p.MaxMembers {
		remaining = p.MaxMembers - used
	}
	return &types.QueryIssuanceResponse{Issuance: log, Used: used, Remaining: remaining}, nil
}
