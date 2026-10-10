package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// TestInitGenesisRefusals pins the import checks Validate cannot make alone: they read the block,
// the account keeper, Bank, and the asset registry.
func (s *KeeperTestSuite) TestInitGenesisRefusals() {
	a, b := testAddress(3), testAddress(4)
	s.Run("nil genesis", func() {
		s.SetupTest()
		s.Require().ErrorContains(s.keeper.InitGenesis(s.ctx, nil), "nil disbursement genesis")
	})
	s.Run("module accounts not set", func() {
		s.SetupTest()
		s.moduleAccountsMissing = true
		fresh, ctx := s.newKeeper()
		s.Require().ErrorContains(fresh.InitGenesis(ctx, types.DefaultGenesisState()), "module account has not been set")
	})
	s.Run("appointment naming the authority", func() {
		s.SetupTest()
		gs := types.DefaultGenesisState()
		gs.GrantsMandate = types.NewDisabledGrantsMandate(1)
		gs.GrantsMandate.Committee, gs.GrantsMandate.ActivationHeight, gs.GrantsMandate.ExpiryHeight = s.authority, 1, 100
		s.Require().NoError(gs.Validate())
		fresh, ctx := s.newKeeper()
		s.Require().ErrorContains(fresh.InitGenesis(ctx, gs), "distinct from the disbursement authority")
	})
	s.Run("member account holding a stablecoin", func() {
		s.SetupTest()
		s.fund(types.MembersName, usd(1))
		fresh, ctx := s.newKeeper()
		s.Require().ErrorContains(fresh.InitGenesis(ctx, types.DefaultGenesisState()), "pays NOAH only")
	})
	s.Run("contributor account holding an unregistered denomination", func() {
		s.SetupTest()
		s.fund(types.ContributorsName, sdk.NewInt64Coin("azzz", 1))
		fresh, ctx := s.newKeeper()
		s.Require().ErrorContains(fresh.InitGenesis(ctx, types.DefaultGenesisState()), "not a registered asset")
	})
	s.Run("beneficiary entry in the future", func() {
		s.SetupTest()
		gs := types.DefaultGenesisState()
		gs.Beneficiaries = []types.Beneficiary{{Address: a, Controller: b, OwnershipCommitted: math.ZeroInt(), Seat: math.ZeroInt()}}
		gs.BeneficiaryJournal = []types.BeneficiaryEntry{{Id: 1, Beneficiary: a, Actor: a, Height: 1, Time: s.now() + 1, Controller: b}}
		gs.NextBeneficiaryEntryId = 2
		s.Require().NoError(gs.Validate())
		fresh, ctx := s.newKeeper()
		s.Require().ErrorContains(fresh.InitGenesis(ctx, gs), "beneficiary entry 1 is in the future")
	})
	s.Run("principal paid past genesis time", func() {
		s.SetupTest()
		// An ownership grant paid its first half before its ten-second cliff: every stamp sits in the
		// past and the shape validates, but at genesis time nothing has accrued.
		start := s.now() - 5
		gs := types.DefaultGenesisState()
		gs.Beneficiaries = []types.Beneficiary{{Address: a, Controller: a, OwnershipCommitted: math.NewInt(20), Seat: math.ZeroInt()}}
		gs.Grants = []types.Grant{{
			Id: 1, Kind: types.GrantKind_GRANT_KIND_OWNERSHIP, Beneficiary: a, Payee: a, CreatedBy: s.authority,
			Amount: noah(20), Schedule: types.Schedule{FirstLength: 10, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 1, Periods: 1},
			StartTime: start, CreatedTime: start, CreatedHeight: 1, Paid: math.NewInt(10), Remaining: math.NewInt(10),
			Reference: "agreement", AwardValue: math.ZeroInt(), DueTime: start + 20,
		}}
		gs.NextGrantId = 2
		book := types.EmptyBook(types.ContributorsName)
		s.Require().NoError(book.Reserve(noah(10)))
		gs.Books = []types.AccountBook{book}
		gs.GrantJournal = []types.GrantEntry{
			{Id: 1, GrantId: 1, Action: types.GrantAction_GRANT_ACTION_CREATED, Actor: s.authority, Height: 1, Time: start, Amount: noah(20), Offer: math.ZeroInt()},
			{Id: 2, GrantId: 1, Action: types.GrantAction_GRANT_ACTION_PAID, Actor: s.authority, Height: 2, Time: start + 1, Amount: noah(10), Subject: a, Offer: math.ZeroInt()},
		}
		gs.NextGrantEntryId = 3
		s.Require().NoError(gs.Validate())
		s.fund(types.ContributorsName, noah(10))
		fresh, ctx := s.newKeeper()
		s.Require().ErrorContains(fresh.InitGenesis(ctx, gs), "paid past genesis time")
	})
}

// TestGenesisRoundTripsAControllerRotation pins that the beneficiary journal imports with the
// record it explains and serves the same query afterwards.
func (s *KeeperTestSuite) TestGenesisRoundTripsAControllerRotation() {
	s.SetupTest()
	a, b := testAddress(3), testAddress(4)
	s.fund(types.ContributorsName, noah(1_000))
	id, err := s.grantOwnership(a, 100, s.now())
	s.Require().NoError(err)
	s.Require().NoError(s.setController(a, a, b))
	exported := s.export()
	s.Require().Len(exported.BeneficiaryJournal, 1)

	fresh, ctx := s.newKeeper()
	s.Require().NoError(fresh.InitGenesis(ctx, exported))
	again, err := fresh.ExportGenesis(ctx)
	s.Require().NoError(err)
	s.Require().Equal(exported, again)

	q := keeper.NewQueryServerImpl(fresh)
	journal, err := q.BeneficiaryJournal(ctx, &types.QueryBeneficiaryJournalRequest{Address: a})
	s.Require().NoError(err)
	s.Require().Len(journal.Entries, 1)
	s.Require().Equal(b, journal.Entries[0].Controller)
	record, err := q.Beneficiary(ctx, &types.QueryBeneficiaryRequest{Address: a})
	s.Require().NoError(err)
	s.Require().Equal(b, record.Beneficiary.Controller)
	grant, err := q.Grant(ctx, &types.QueryGrantRequest{Id: id})
	s.Require().NoError(err)
	s.Require().Equal(s.grant(id), grant.Grant)
}
