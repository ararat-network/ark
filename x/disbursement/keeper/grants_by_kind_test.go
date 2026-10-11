package keeper_test

import (
	"context"

	"google.golang.org/grpc/codes"

	"cosmossdk.io/collections"

	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// kindIDs lists the grant IDs a keeper's kind index holds under one kind, in ID order.
func (s *KeeperTestSuite) kindIDs(ctx context.Context, k *keeper.Keeper, kind types.GrantKind) []uint64 {
	var ids []uint64
	s.Require().NoError(k.GrantsByKind.Walk(ctx, collections.NewPrefixedPairRange[int32, uint64](int32(kind)), func(key collections.Pair[int32, uint64]) (bool, error) {
		ids = append(ids, key.K2())
		return false, nil
	}))
	return ids
}

// TestGrantsByKindIndex checks every grant lands under its kind, the query pages one kind through
// the index and refuses a second filter or an unknown kind, and import rebuilds the index from the
// grant list alone.
func (s *KeeperTestSuite) TestGrantsByKindIndex() {
	_, c, _ := s.grantsFixture()
	byKind := map[types.GrantKind][]uint64{
		types.GrantKind_GRANT_KIND_MEMBER:       {1, 2},
		types.GrantKind_GRANT_KIND_COMPENSATION: {3},
		types.GrantKind_GRANT_KIND_OWNERSHIP:    {4},
	}
	for kind, want := range byKind {
		s.Require().Equal(want, s.kindIDs(s.ctx, s.keeper, kind), kind.String())
		res, err := s.queryServer.Grants(s.ctx, &types.QueryGrantsRequest{Kind: kind})
		s.Require().NoError(err)
		s.Require().Equal(want, grantIDs(res.Grants), kind.String())
	}

	for name, req := range map[string]*types.QueryGrantsRequest{
		"kind and beneficiary":    {Kind: types.GrantKind_GRANT_KIND_OWNERSHIP, Beneficiary: c},
		"kind and committee term": {Kind: types.GrantKind_GRANT_KIND_COMPENSATION, CommitteeTerm: s.term},
		"kind and registrar term": {Kind: types.GrantKind_GRANT_KIND_MEMBER, RegistrarTerm: s.registrarTerm},
		"unknown kind":            {Kind: types.GrantKind(7)},
	} {
		s.Run(name, func() {
			_, err := s.queryServer.Grants(s.ctx, req)
			s.requireCode(err, codes.InvalidArgument)
		})
	}

	gs := s.export()
	fresh, ctx := s.newKeeper()
	s.Require().NoError(fresh.InitGenesis(ctx, gs))
	for kind, want := range byKind {
		s.Require().Equal(want, s.kindIDs(ctx, fresh, kind), kind.String())
	}
}
