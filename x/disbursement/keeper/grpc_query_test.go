package keeper_test

import (
	"google.golang.org/grpc/codes"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/pagination"
	"github.com/ararat-network/ark/x/disbursement/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	s.SetupTest()
	_, err := s.queryServer.Params(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)

	res, err := s.queryServer.Params(s.ctx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(testParams(), res.Params)
}

func (s *KeeperTestSuite) TestQueryGrantsMandate() {
	s.Run("nil request", func() {
		s.SetupTest()
		_, err := s.queryServer.GrantsMandate(s.ctx, nil)
		s.requireCode(err, codes.InvalidArgument)
	})
	s.Run("a disabled mandate is inactive with no award power", func() {
		s.SetupTest()
		res, err := s.queryServer.GrantsMandate(s.ctx, &types.QueryGrantsMandateRequest{})
		s.Require().NoError(err)
		s.Require().True(res.Mandate.IsDisabled())
		s.Require().False(res.Active)
		s.Require().True(res.CompensationUsed.IsZero())
		s.Require().True(res.CompensationRemaining.IsZero())
	})
	s.Run("an appointment reports its term's usage and room over its window", func() {
		s.SetupTest()
		s.appointCommittee(500)
		s.Require().NoError(s.keeper.CompensationUsed.Set(s.ctx, math.NewInt(200)))
		res, err := s.queryServer.GrantsMandate(s.ctx, &types.QueryGrantsMandateRequest{})
		s.Require().NoError(err)
		s.Require().Equal(s.term, res.Mandate.Term)
		s.Require().True(res.Active)
		s.Require().Equal(math.NewInt(200), res.CompensationUsed)
		s.Require().Equal(math.NewInt(300), res.CompensationRemaining)
		// The window is half-open: expiry is the first inactive time.
		s.setTime(res.Mandate.ExpiryTime)
		res, err = s.queryServer.GrantsMandate(s.ctx, &types.QueryGrantsMandateRequest{})
		s.Require().NoError(err)
		s.Require().False(res.Active)
	})
	s.Run("usage past the allowance is an invariant failure", func() {
		s.SetupTest()
		s.appointCommittee(500)
		s.Require().NoError(s.keeper.CompensationUsed.Set(s.ctx, math.NewInt(501)))
		_, err := s.queryServer.GrantsMandate(s.ctx, &types.QueryGrantsMandateRequest{})
		s.Require().ErrorContains(err, "exceeds the term's allowance")
	})
}

func (s *KeeperTestSuite) TestQueryRegistrarMandate() {
	s.Run("nil request", func() {
		s.SetupTest()
		_, err := s.queryServer.RegistrarMandate(s.ctx, nil)
		s.requireCode(err, codes.InvalidArgument)
	})
	s.Run("a disabled registrar is inactive", func() {
		s.SetupTest()
		res, err := s.queryServer.RegistrarMandate(s.ctx, &types.QueryRegistrarMandateRequest{})
		s.Require().NoError(err)
		s.Require().True(res.Mandate.IsDisabled())
		s.Require().False(res.Active)
	})
	s.Run("an appointment is active inside its window", func() {
		s.SetupTest()
		s.appointCommittee(0)
		s.appointRegistrar()
		res, err := s.queryServer.RegistrarMandate(s.ctx, &types.QueryRegistrarMandateRequest{})
		s.Require().NoError(err)
		s.Require().Equal(s.registrar, res.Mandate.Committee)
		s.Require().Equal(s.registrarTerm, res.Mandate.Term)
		s.Require().True(res.Active)
		s.setTime(res.Mandate.ExpiryTime)
		res, err = s.queryServer.RegistrarMandate(s.ctx, &types.QueryRegistrarMandateRequest{})
		s.Require().NoError(err)
		s.Require().False(res.Active)
	})
}

func (s *KeeperTestSuite) TestQueryBalance() {
	contributors, members := types.ContributorsName, types.MembersName
	row := func(account string, balance, reserved, uncommitted sdk.Coin) types.AccountBalance {
		return types.AccountBalance{Account: account, Balance: balance, Reserved: reserved, Uncommitted: uncommitted}
	}
	s.Run("nil request", func() {
		s.SetupTest()
		_, err := s.queryServer.Balance(s.ctx, nil)
		s.requireCode(err, codes.InvalidArgument)
	})
	s.Run("malformed denomination", func() {
		s.SetupTest()
		_, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{Denom: "!"})
		s.requireCode(err, codes.InvalidArgument)
	})
	s.Run("empty custody lists NOAH for both accounts", func() {
		s.SetupTest()
		res, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
		s.Require().NoError(err)
		s.Require().Equal([]types.AccountBalance{
			row(contributors, noah(0), noah(0), noah(0)),
			row(members, noah(0), noah(0), noah(0)),
		}, res.Accounts)
		s.Require().True(res.MemberTranche.IsZero())
	})
	s.Run("custody, reservations, uncommitted custody, and the tranche", func() {
		s.SetupTest()
		s.appointCommittee(0)
		s.appointRegistrar()
		s.openTranche(30_000)
		s.register(testAddress(3))
		s.fund(contributors, noah(100), usd(50))
		res, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
		s.Require().NoError(err)
		// The member's 10,000 was drawn from the tranche and reserved; its tenth waits for the hold.
		s.Require().Equal([]types.AccountBalance{
			row(contributors, noah(100), noah(0), noah(100)),
			row(contributors, usd(50), usd(0), usd(50)),
			row(members, noah(30_000), noah(10_000), noah(20_000)),
		}, res.Accounts)
		s.Require().Equal(math.NewInt(20_000), res.MemberTranche)

		// One denomination lists only the accounts that may hold it: the member account pays NOAH only.
		res, err = s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{Denom: "ausd"})
		s.Require().NoError(err)
		s.Require().Equal([]types.AccountBalance{row(contributors, usd(50), usd(0), usd(50))}, res.Accounts)
		res, err = s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{Denom: chain.NoahBaseDenom})
		s.Require().NoError(err)
		s.Require().Len(res.Accounts, 2)
		res, err = s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{Denom: "azzz"})
		s.Require().NoError(err)
		s.Require().Equal([]types.AccountBalance{row(contributors, sdk.NewInt64Coin("azzz", 0), sdk.NewInt64Coin("azzz", 0), sdk.NewInt64Coin("azzz", 0))}, res.Accounts)
	})
	s.Run("custody below its reservations is an invariant failure", func() {
		s.SetupTest()
		book := types.EmptyBook(contributors)
		s.Require().NoError(book.Reserve(noah(1)))
		s.Require().NoError(s.keeper.Books.Set(s.ctx, contributors, book))
		_, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
		s.Require().ErrorContains(err, "custody is below its commitments")
	})
}

func (s *KeeperTestSuite) TestQueryGrant() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	id := s.register(testAddress(3))

	_, err := s.queryServer.Grant(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)
	_, err = s.queryServer.Grant(s.ctx, &types.QueryGrantRequest{})
	s.requireCode(err, codes.InvalidArgument)
	_, err = s.queryServer.Grant(s.ctx, &types.QueryGrantRequest{Id: id + 1})
	s.Require().ErrorIs(err, types.ErrGrantNotFound)
	s.requireCode(err, codes.NotFound)

	res, err := s.queryServer.Grant(s.ctx, &types.QueryGrantRequest{Id: id})
	s.Require().NoError(err)
	s.Require().Equal(s.grant(id), res.Grant)
	s.Require().Equal(types.GrantKind_GRANT_KIND_MEMBER, res.Grant.Kind)
}

// grantsFixture registers two members under the registrar's term and awards a third address
// committee compensation and governance ownership, four grants in ID order.
func (s *KeeperTestSuite) grantsFixture() (a, c string, ids []uint64) {
	s.SetupTest()
	s.appointCommittee(10_000)
	s.appointRegistrar()
	s.openTranche(20_000)
	s.fund(types.ContributorsName, noah(1_000))
	a, c = testAddress(3), testAddress(5)
	members, err := s.registerMembers(a, testAddress(4))
	s.Require().NoError(err)
	award, err := s.committeeCompensate(c, noah(100), 1)
	s.Require().NoError(err)
	own, err := s.grantOwnership(c, 100, s.now())
	s.Require().NoError(err)
	ids = append(members, award, own)
	s.Require().Equal([]uint64{1, 2, 3, 4}, ids)
	return a, c, ids
}

func grantIDs(grants []types.Grant) []uint64 {
	ids := make([]uint64, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.Id)
	}
	return ids
}

func (s *KeeperTestSuite) TestQueryGrants() {
	_, c, _ := s.grantsFixture()
	list := func(req *types.QueryGrantsRequest) []uint64 {
		res, err := s.queryServer.Grants(s.ctx, req)
		s.Require().NoError(err)
		return grantIDs(res.Grants)
	}
	s.Require().Equal([]uint64{1, 2, 3, 4}, list(&types.QueryGrantsRequest{}), "unfiltered, in ID order")
	s.Require().Equal([]uint64{3, 4}, list(&types.QueryGrantsRequest{Beneficiary: c}))
	s.Require().Equal([]uint64{3}, list(&types.QueryGrantsRequest{CommitteeTerm: s.term}), "the committee's awards")
	s.Require().Equal([]uint64{1, 2}, list(&types.QueryGrantsRequest{RegistrarTerm: s.registrarTerm}), "the registrar's registrations")
	s.Require().Empty(list(&types.QueryGrantsRequest{CommitteeTerm: s.term + 1}))

	for name, req := range map[string]*types.QueryGrantsRequest{
		"beneficiary and committee term": {Beneficiary: c, CommitteeTerm: s.term},
		"beneficiary and registrar term": {Beneficiary: c, RegistrarTerm: s.registrarTerm},
		"both terms":                     {CommitteeTerm: s.term, RegistrarTerm: s.registrarTerm},
		"malformed beneficiary":          {Beneficiary: "nobody"},
		"offset":                         {Pagination: &sdkquery.PageRequest{Offset: 1}},
		"total":                          {Pagination: &sdkquery.PageRequest{CountTotal: true}},
		"limit past the cap":             {Pagination: &sdkquery.PageRequest{Limit: pagination.MaxLimit + 1}},
	} {
		s.Run(name, func() {
			_, err := s.queryServer.Grants(s.ctx, req)
			s.requireCode(err, codes.InvalidArgument)
		})
	}
	_, err := s.queryServer.Grants(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)

	// A page of one walks the whole list by key.
	var seen []uint64
	var key []byte
	for range 4 {
		res, err := s.queryServer.Grants(s.ctx, &types.QueryGrantsRequest{Pagination: &sdkquery.PageRequest{Limit: 1, Key: key}})
		s.Require().NoError(err)
		s.Require().Len(res.Grants, 1)
		seen = append(seen, res.Grants[0].Id)
		key = res.Pagination.NextKey
	}
	s.Require().Equal([]uint64{1, 2, 3, 4}, seen)
	s.Require().Empty(key)
}

func (s *KeeperTestSuite) TestQueryGrantJournal() {
	s.grantsFixture()
	created, paid := types.GrantAction_GRANT_ACTION_CREATED, types.GrantAction_GRANT_ACTION_PAID
	s.Require().Equal([]types.GrantAction{created}, s.actions(1), "a member's registration pays nothing inside its hold")
	s.Require().Equal([]types.GrantAction{created}, s.actions(3), "an award pays nothing before its first boundary")
	// Every first period is the two-week floor, so one look past it pays all four.
	s.advance(int64(hold))
	s.endBlock()
	s.Require().Equal([]types.GrantAction{created, paid}, s.actions(1), "the block pays the tenth once the hold ends")

	// Unfiltered, the journal lists every grant's run in grant then entry order.
	res, err := s.queryServer.GrantJournal(s.ctx, &types.QueryGrantJournalRequest{})
	s.Require().NoError(err)
	var grants []uint64
	for _, e := range res.Entries {
		grants = append(grants, e.GrantId)
	}
	s.Require().Equal([]uint64{1, 1, 2, 2, 3, 3, 4, 4}, grants)

	_, err = s.queryServer.GrantJournal(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)
	_, err = s.queryServer.GrantJournal(s.ctx, &types.QueryGrantJournalRequest{Pagination: &sdkquery.PageRequest{Offset: 2}})
	s.requireCode(err, codes.InvalidArgument)
}

func (s *KeeperTestSuite) TestQueryMember() {
	a, c, ids := s.grantsFixture()
	_, err := s.queryServer.Member(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)
	_, err = s.queryServer.Member(s.ctx, &types.QueryMemberRequest{Address: "nobody"})
	s.requireCode(err, codes.InvalidArgument)
	// A beneficiary with awards but no registration holds no member grant.
	_, err = s.queryServer.Member(s.ctx, &types.QueryMemberRequest{Address: c})
	s.Require().ErrorIs(err, types.ErrMemberNotFound)
	s.requireCode(err, codes.NotFound)

	res, err := s.queryServer.Member(s.ctx, &types.QueryMemberRequest{Address: a})
	s.Require().NoError(err)
	s.Require().Equal(ids[0], res.Grant.Id)
	s.Require().Equal(a, res.Grant.Beneficiary)
}

func (s *KeeperTestSuite) TestQueryMembers() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(30_000)
	// Registered out of address order, listed in it.
	for _, seed := range []byte{5, 3, 4} {
		s.register(testAddress(seed))
	}
	res, err := s.queryServer.Members(s.ctx, &types.QueryMembersRequest{})
	s.Require().NoError(err)
	var members []string
	for _, g := range res.Grants {
		members = append(members, g.Beneficiary)
	}
	s.Require().Equal([]string{testAddress(3), testAddress(4), testAddress(5)}, members)

	page, err := s.queryServer.Members(s.ctx, &types.QueryMembersRequest{Pagination: &sdkquery.PageRequest{Limit: 2}})
	s.Require().NoError(err)
	s.Require().Len(page.Grants, 2)
	s.Require().NotEmpty(page.Pagination.NextKey)
	_, err = s.queryServer.Members(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)
}

func (s *KeeperTestSuite) TestQueryBeneficiary() {
	s.SetupTest()
	a := testAddress(3)
	_, err := s.queryServer.Beneficiary(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)
	_, err = s.queryServer.Beneficiary(s.ctx, &types.QueryBeneficiaryRequest{Address: "nobody"})
	s.requireCode(err, codes.InvalidArgument)
	_, err = s.queryServer.Beneficiary(s.ctx, &types.QueryBeneficiaryRequest{Address: a})
	s.Require().ErrorIs(err, types.ErrBeneficiaryNotFound)
	s.requireCode(err, codes.NotFound)

	s.fund(types.ContributorsName, noah(1_000))
	_, err = s.grantOwnership(a, 100, s.now())
	s.Require().NoError(err)
	res, err := s.queryServer.Beneficiary(s.ctx, &types.QueryBeneficiaryRequest{Address: a})
	s.Require().NoError(err)
	s.Require().Equal(a, res.Beneficiary.Controller)
	s.Require().Equal(math.NewInt(100), res.Beneficiary.OwnershipCommitted)
	s.Require().Equal(testParams().OwnershipCap.SubRaw(100), res.OwnershipRemaining)

	// A cap lowered under the record's commitment leaves no room rather than a negative figure.
	p := testParams()
	p.OwnershipCap = math.NewInt(50)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, p))
	res, err = s.queryServer.Beneficiary(s.ctx, &types.QueryBeneficiaryRequest{Address: a})
	s.Require().NoError(err)
	s.Require().True(res.OwnershipRemaining.IsZero())
}

func (s *KeeperTestSuite) TestQueryBeneficiaryJournal() {
	s.SetupTest()
	a, b := testAddress(3), testAddress(4)
	s.fund(types.ContributorsName, noah(1_000))
	_, err := s.grantOwnership(a, 100, s.now())
	s.Require().NoError(err)
	s.Require().NoError(s.setController(a, a, b))

	res, err := s.queryServer.BeneficiaryJournal(s.ctx, &types.QueryBeneficiaryJournalRequest{Address: a})
	s.Require().NoError(err)
	s.Require().Len(res.Entries, 1)
	s.Require().Equal(b, res.Entries[0].Controller)
	s.Require().Equal(a, res.Entries[0].Actor)
	// The controller's own address has no record and no history.
	res, err = s.queryServer.BeneficiaryJournal(s.ctx, &types.QueryBeneficiaryJournalRequest{Address: b})
	s.Require().NoError(err)
	s.Require().Empty(res.Entries)
	_, err = s.queryServer.BeneficiaryJournal(s.ctx, &types.QueryBeneficiaryJournalRequest{Address: "nobody"})
	s.requireCode(err, codes.InvalidArgument)
	_, err = s.queryServer.BeneficiaryJournal(s.ctx, nil)
	s.requireCode(err, codes.InvalidArgument)
}
