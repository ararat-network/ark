package keeper

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/pagination"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// queryServer validates each request itself and otherwise returns the keeper's error: a registered
// error carries its gRPC code, so no handler classifies.
type queryServer struct {
	k *Keeper
}

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl exposes indexed grant queries.
func NewQueryServerImpl(k *Keeper) types.QueryServer {
	return queryServer{k: k}
}

func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	p, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting disbursement params: %w", err)
	}
	return &types.QueryParamsResponse{Params: p}, nil
}

func (q queryServer) GrantsMandate(ctx context.Context, req *types.QueryGrantsMandateRequest) (*types.QueryGrantsMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	appointment, err := q.k.GrantsMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting grants mandate: %w", err)
	}
	compensation, err := q.k.CompensationUsed.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting compensation usage: %w", err)
	}
	remaining, err := appointment.CompensationAllowance.SafeSub(compensation)
	if err != nil || remaining.IsNegative() {
		return nil, fmt.Errorf("compensation usage %s exceeds the term's allowance %s", compensation, appointment.CompensationAllowance)
	}
	return &types.QueryGrantsMandateResponse{
		Mandate:               appointment,
		Active:                appointment.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
		CompensationUsed:      compensation,
		CompensationRemaining: remaining,
	}, nil
}

func (q queryServer) RegistrarMandate(ctx context.Context, req *types.QueryRegistrarMandateRequest) (*types.QueryRegistrarMandateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	appointment, err := q.k.RegistrarMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting registrar mandate: %w", err)
	}
	return &types.QueryRegistrarMandateResponse{
		Mandate: appointment,
		Active:  appointment.IsActive(uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())),
	}, nil
}

func (q queryServer) Balance(ctx context.Context, req *types.QueryBalanceRequest) (*types.QueryBalanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Denom != "" {
		if err := sdk.ValidateDenom(req.Denom); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	tranche, err := q.k.MemberTranche.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting the member tranche: %w", err)
	}
	res := &types.QueryBalanceResponse{MemberTranche: tranche}
	for _, account := range types.PoolAccountNames() {
		b, err := q.k.getBook(ctx, account)
		if err != nil {
			return nil, fmt.Errorf("getting %s book: %w", account, err)
		}
		for _, balance := range q.custody(ctx, account, req.Denom) {
			uncommitted, err := b.Uncommitted(balance)
			if err != nil {
				return nil, fmt.Errorf("getting %s balance: %w", balance.Denom, err)
			}
			res.Accounts = append(res.Accounts, types.AccountBalance{
				Account:     account,
				Balance:     balance,
				Reserved:    sdk.NewCoin(balance.Denom, b.Reserved.AmountOf(balance.Denom)),
				Uncommitted: sdk.NewCoin(balance.Denom, uncommitted),
			})
		}
	}
	return res, nil
}

// custody lists the balances to report: the requested denomination, or NOAH, even at zero, then every
// other denomination the account holds. The send restriction admits only NOAH and registered assets,
// so that is all of custody.
func (q queryServer) custody(ctx context.Context, account, denom string) []sdk.Coin {
	address := q.k.accountKeeper.GetModuleAddress(account)
	if denom != "" {
		if account == types.MembersName && denom != chain.NoahBaseDenom {
			return nil
		}
		return []sdk.Coin{q.k.bankKeeper.GetBalance(ctx, address, denom)}
	}
	balances := []sdk.Coin{q.k.bankKeeper.GetBalance(ctx, address, chain.NoahBaseDenom)}
	for _, c := range q.k.bankKeeper.GetAllBalances(ctx, address) {
		if c.Denom != chain.NoahBaseDenom {
			balances = append(balances, c)
		}
	}
	return balances
}

func (q queryServer) Grant(ctx context.Context, req *types.QueryGrantRequest) (*types.QueryGrantResponse, error) {
	if req == nil || req.Id == 0 {
		return nil, status.Error(codes.InvalidArgument, "grant ID required")
	}
	g, err := q.k.getGrant(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &types.QueryGrantResponse{Grant: g}, nil
}

func (q queryServer) Grants(ctx context.Context, req *types.QueryGrantsRequest) (*types.QueryGrantsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	p, err := pagination.Bounded(req.Pagination)
	if err != nil {
		return nil, err
	}
	var grants []types.Grant
	var response *sdkquery.PageResponse
	byTerm := func(key collections.Pair[uint64, uint64], _ collections.NoValue) (types.Grant, error) {
		return q.k.Grants.Get(ctx, key.K2())
	}
	switch {
	case (req.Beneficiary != "" && (req.CommitteeTerm != 0 || req.RegistrarTerm != 0)) || (req.CommitteeTerm != 0 && req.RegistrarTerm != 0):
		return nil, status.Error(codes.InvalidArgument, "filter by beneficiary, committee term, or registrar term, not two")
	case req.CommitteeTerm != 0:
		grants, response, err = sdkquery.CollectionPaginate(ctx, q.k.CommitteeTermGrants, p, byTerm, sdkquery.WithCollectionPaginationPairPrefix[uint64, uint64](req.CommitteeTerm))
	case req.RegistrarTerm != 0:
		grants, response, err = sdkquery.CollectionPaginate(ctx, q.k.RegistrarTermGrants, p, byTerm, sdkquery.WithCollectionPaginationPairPrefix[uint64, uint64](req.RegistrarTerm))
	case req.Beneficiary == "":
		grants, response, err = sdkquery.CollectionPaginate(ctx, q.k.Grants, p, func(_ uint64, g types.Grant) (types.Grant, error) { return g, nil })
	default:
		a, parseErr := chain.ParseCanonicalAccountAddress("beneficiary", req.Beneficiary)
		if parseErr != nil {
			return nil, status.Error(codes.InvalidArgument, parseErr.Error())
		}
		grants, response, err = sdkquery.CollectionPaginate(ctx, q.k.GrantsByBeneficiary, p, func(key collections.Pair[sdk.AccAddress, uint64], _ bool) (types.Grant, error) {
			return q.k.Grants.Get(ctx, key.K2())
		}, sdkquery.WithCollectionPaginationPairPrefix[sdk.AccAddress, uint64](a))
	}
	if err != nil {
		return nil, fmt.Errorf("listing grants: %w", err)
	}
	return &types.QueryGrantsResponse{Grants: grants, Pagination: response}, nil
}

func (q queryServer) GrantJournal(ctx context.Context, req *types.QueryGrantJournalRequest) (*types.QueryGrantJournalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	p, err := pagination.Bounded(req.Pagination)
	if err != nil {
		return nil, err
	}
	var entries []types.GrantEntry
	var response *sdkquery.PageResponse
	keep := func(_ collections.Pair[uint64, uint64], e types.GrantEntry) (types.GrantEntry, error) { return e, nil }
	if req.GrantId != 0 {
		entries, response, err = sdkquery.CollectionPaginate(ctx, q.k.GrantJournal, p, keep, sdkquery.WithCollectionPaginationPairPrefix[uint64, uint64](req.GrantId))
	} else {
		entries, response, err = sdkquery.CollectionPaginate(ctx, q.k.GrantJournal, p, keep)
	}
	if err != nil {
		return nil, fmt.Errorf("listing grant journal: %w", err)
	}
	return &types.QueryGrantJournalResponse{Entries: entries, Pagination: response}, nil
}

func (q queryServer) Member(ctx context.Context, req *types.QueryMemberRequest) (*types.QueryMemberResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	a, err := chain.ParseCanonicalAccountAddress("member", req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	g, err := q.k.getMember(ctx, a)
	if err != nil {
		return nil, err
	}
	return &types.QueryMemberResponse{Grant: g}, nil
}

func (q queryServer) Members(ctx context.Context, req *types.QueryMembersRequest) (*types.QueryMembersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	p, err := pagination.Bounded(req.Pagination)
	if err != nil {
		return nil, err
	}
	grants, response, err := sdkquery.CollectionPaginate(ctx, q.k.Members, p, func(_ sdk.AccAddress, id uint64) (types.Grant, error) {
		return q.k.Grants.Get(ctx, id)
	})
	if err != nil {
		return nil, fmt.Errorf("listing members: %w", err)
	}
	return &types.QueryMembersResponse{Grants: grants, Pagination: response}, nil
}

func (q queryServer) Beneficiary(ctx context.Context, req *types.QueryBeneficiaryRequest) (*types.QueryBeneficiaryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	a, err := chain.ParseCanonicalAccountAddress("beneficiary", req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := q.k.getBeneficiary(ctx, a)
	if err != nil {
		return nil, err
	}
	remaining, err := q.k.ownershipRemaining(ctx, p.OwnershipCommitted)
	if err != nil {
		return nil, fmt.Errorf("reading the ownership cap for %s: %w", req.Address, err)
	}
	return &types.QueryBeneficiaryResponse{Beneficiary: p, OwnershipRemaining: remaining}, nil
}

func (q queryServer) BeneficiaryJournal(ctx context.Context, req *types.QueryBeneficiaryJournalRequest) (*types.QueryBeneficiaryJournalResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	address, err := chain.ParseCanonicalAccountAddress("beneficiary", req.Address)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := pagination.Bounded(req.Pagination)
	if err != nil {
		return nil, err
	}
	entries, response, err := sdkquery.CollectionPaginate(ctx, q.k.BeneficiaryJournal, p, func(_ collections.Pair[sdk.AccAddress, uint64], e types.BeneficiaryEntry) (types.BeneficiaryEntry, error) {
		return e, nil
	}, sdkquery.WithCollectionPaginationPairPrefix[sdk.AccAddress, uint64](address))
	if err != nil {
		return nil, fmt.Errorf("listing beneficiary journal: %w", err)
	}
	return &types.QueryBeneficiaryJournalResponse{Entries: entries, Pagination: response}, nil
}
