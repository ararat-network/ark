package keeper_test

import (
	"context"

	"google.golang.org/grpc/codes"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/disbursement/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
)

func (s *KeeperTestSuite) TestNilMessages() {
	s.SetupTest()
	for name, call := range map[string]func(context.Context) error{
		"UpdateParams":        func(ctx context.Context) error { _, err := s.msgServer.UpdateParams(ctx, nil); return err },
		"SetGrantsMandate":    func(ctx context.Context) error { _, err := s.msgServer.SetGrantsMandate(ctx, nil); return err },
		"OpenTranche":         func(ctx context.Context) error { _, err := s.msgServer.OpenTranche(ctx, nil); return err },
		"ReturnUnallocated":   func(ctx context.Context) error { _, err := s.msgServer.ReturnUnallocated(ctx, nil); return err },
		"GrantOwnership":      func(ctx context.Context) error { _, err := s.msgServer.GrantOwnership(ctx, nil); return err },
		"Compensate":          func(ctx context.Context) error { _, err := s.msgServer.Compensate(ctx, nil); return err },
		"CancelGrants":        func(ctx context.Context) error { _, err := s.msgServer.CancelGrants(ctx, nil); return err },
		"ReinstateMembers":    func(ctx context.Context) error { _, err := s.msgServer.ReinstateMembers(ctx, nil); return err },
		"CommitteeRegistrar":  func(ctx context.Context) error { _, err := s.msgServer.CommitteeRegistrar(ctx, nil); return err },
		"RegisterMembers":     func(ctx context.Context) error { _, err := s.msgServer.RegisterMembers(ctx, nil); return err },
		"CommitteeSuspend":    func(ctx context.Context) error { _, err := s.msgServer.CommitteeSuspend(ctx, nil); return err },
		"CommitteeReinstate":  func(ctx context.Context) error { _, err := s.msgServer.CommitteeReinstate(ctx, nil); return err },
		"CommitteeCompensate": func(ctx context.Context) error { _, err := s.msgServer.CommitteeCompensate(ctx, nil); return err },
		"SetPayee":            func(ctx context.Context) error { _, err := s.msgServer.SetPayee(ctx, nil); return err },
		"SetController":       func(ctx context.Context) error { _, err := s.msgServer.SetController(ctx, nil); return err },
	} {
		s.Run(name, func() { s.Require().ErrorContains(call(s.ctx), "nil") })
	}
}

func (s *KeeperTestSuite) TestGovernanceMessagesRequireTheAuthority() {
	s.SetupTest()
	stranger := testAddress(9)
	for _, msg := range []sdk.Msg{
		&types.MsgUpdateParams{Authority: stranger, Params: testParams()},
		&types.MsgSetGrantsMandate{Authority: stranger, Committee: s.committee, ActivationHeight: 1, ExpiryHeight: 100},
		&types.MsgOpenTranche{Authority: stranger, Amount: math.OneInt()},
		&types.MsgReturnUnallocated{Authority: stranger, Amount: noah(1)},
		&types.MsgGrantOwnership{Authority: stranger, Beneficiary: testAddress(3), Amount: math.OneInt(), StartTime: s.now(), Reference: "x"},
		&types.MsgCompensate{Authority: stranger, Beneficiary: testAddress(3), Amount: noah(1), StartTime: s.now(), Periods: 1, Reference: "x"},
		&types.MsgCancelGrants{Authority: stranger, GrantIds: []uint64{1}},
		&types.MsgReinstateMembers{Authority: stranger, Addresses: []string{testAddress(3)}},
	} {
		s.Run(sdk.MsgTypeURL(msg), func() { s.Require().ErrorIs(s.deliver(msg), errortypes.ErrUnauthorized) })
	}
}

func (s *KeeperTestSuite) TestUpdateParams() {
	s.SetupTest()
	p := testParams()
	p.CompensationWindow = 0
	s.Require().ErrorContains(s.deliver(&types.MsgUpdateParams{Authority: s.authority, Params: p}), "compensation window")
	p.CompensationWindow, p.OwnershipCap = 1, math.NewInt(7)
	s.Require().NoError(s.deliver(&types.MsgUpdateParams{Authority: s.authority, Params: p}))
	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(p, stored)
}

func (s *KeeperTestSuite) TestOpenTranche() {
	s.SetupTest()
	open := func(amount int64) error {
		return s.deliver(&types.MsgOpenTranche{Authority: s.authority, Amount: math.NewInt(amount)})
	}
	s.fund(types.MembersName, noah(20_000))
	s.Require().ErrorContains(open(0), "positive")
	s.Require().ErrorContains(open(20_001), "exceeds the member pool")
	s.Require().NoError(open(5_000))
	s.Require().NoError(open(15_000), "openings add up to the pool")
	s.Require().ErrorContains(open(1), "exceeds the member pool")
	tranche, err := s.keeper.MemberTranche.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(20_000), tranche)
}

func (s *KeeperTestSuite) TestReturnUnallocated() {
	s.SetupTest()
	ret := func(coin sdk.Coin) error {
		return s.deliver(&types.MsgReturnUnallocated{Authority: s.authority, Amount: coin})
	}
	s.fund(types.ContributorsName, noah(1_000), usd(30))
	s.Require().Error(ret(sdk.Coin{Denom: "!", Amount: math.OneInt()}))
	s.Require().ErrorContains(ret(noah(0)), "positive")
	s.Require().ErrorContains(ret(usd(31)), "uncommitted contributor custody")
	// Reserved principal never returns: an award holds 600 of the 1,000.
	_, err := s.grantOwnership(testAddress(3), 600, s.now())
	s.Require().NoError(err)
	s.Require().ErrorContains(ret(noah(401)), "uncommitted contributor custody")
	s.Require().NoError(ret(noah(400)))
	s.Require().NoError(ret(usd(30)))
	s.Require().Equal(sdk.NewCoins(noah(400), usd(30)), s.communityPool)
	s.Require().Equal(sdk.NewCoins(noah(600)), s.balances[types.ContributorsName])
}

func (s *KeeperTestSuite) TestGrantOwnership() {
	s.SetupTest()
	a := testAddress(3)
	s.fund(types.ContributorsName, noah(1_000))
	_, err := s.grantOwnership(a, 1, s.now()-1)
	s.Require().ErrorContains(err, "no earlier than its award")
	_, err = s.grantOwnership(a, 0, s.now())
	s.Require().ErrorContains(err, "positive")
	_, err = s.grantOwnership(authtypes.NewModuleAddress(types.ContributorsName).String(), 1, s.now())
	s.Require().ErrorContains(err, "cannot receive funds")
	s.blocked[a] = true
	_, err = s.grantOwnership(a, 1, s.now())
	s.Require().ErrorContains(err, "cannot receive funds")
	s.blocked[a] = false
	_, err = s.grantOwnership(a, 1_001, s.now())
	s.Require().ErrorContains(err, "insufficient uncommitted")

	id, err := s.grantOwnership(a, 100, s.now())
	s.Require().NoError(err)
	g := s.grant(id)
	s.Require().Equal(s.now()+10, g.DueTime, "queued for its first boundary")
	s.Require().Zero(g.MandateTerm)
	s.Require().True(g.Paid.IsZero())
	record, err := s.keeper.Beneficiaries.Get(s.ctx, sdk.MustAccAddressFromBech32(a))
	s.Require().NoError(err)
	s.Require().Equal(a, record.Controller, "a first award opens the record under its own control")
	s.Require().Equal(math.NewInt(100), record.OwnershipCommitted)
	// The cap binds the record over every grant.
	p := testParams()
	p.OwnershipCap = math.NewInt(150)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, p))
	_, err = s.grantOwnership(a, 51, s.now())
	s.Require().ErrorContains(err, "remaining ownership")
	_, err = s.grantOwnership(a, 50, s.now())
	s.Require().NoError(err)
}

func (s *KeeperTestSuite) TestCompensate() {
	a := testAddress(3)
	s.Run("no reference denomination to value in", func() {
		s.SetupTest()
		s.reference = ""
		_, err := s.compensate(a, noah(10), s.now(), 1, math.ZeroInt())
		s.Require().ErrorContains(err, "no reference denomination")
	})
	s.Run("unregistered denomination", func() {
		s.SetupTest()
		_, err := s.compensate(a, sdk.NewInt64Coin("azzz", 10), s.now(), 1, math.ZeroInt())
		s.Require().ErrorContains(err, "compensation asset")
	})
	s.Run("suspended asset", func() {
		s.SetupTest()
		s.assets["ausd"] = assettypes.Asset{Denom: "ausd", Status: assettypes.AssetStatus_ASSET_STATUS_SUSPENDED}
		_, err := s.compensate(a, usd(10), s.now(), 1, math.ZeroInt())
		s.Require().ErrorContains(err, "active asset")
	})
	s.Run("unpriced asset", func() {
		s.SetupTest()
		delete(s.rates, "ausd")
		s.fund(types.ContributorsName, usd(10))
		_, err := s.compensate(a, usd(10), s.now(), 1, math.ZeroInt())
		s.Require().ErrorContains(err, "valuing the award")
	})
	s.Run("shape", func() {
		s.SetupTest()
		s.fund(types.ContributorsName, noah(100))
		_, err := s.compensate(a, noah(10), s.now(), 0, math.ZeroInt())
		s.Require().ErrorContains(err, "at least one period")
		_, err = s.compensate(a, noah(10), s.now()-types.MaxCompensationBackdate-1, 1, math.ZeroInt())
		s.Require().ErrorContains(err, "at most a quarter")
		_, err = s.compensate(a, noah(10), s.now(), 1, math.OneInt())
		s.Require().ErrorContains(err, "converts nothing")
		_, err = s.compensate(a, noah(0), s.now(), 1, math.ZeroInt())
		s.Require().ErrorContains(err, "positive")
	})
	s.Run("a NOAH award draws on the pool and waits a period", func() {
		s.SetupTest()
		s.fund(types.ContributorsName, noah(100))
		id, err := s.compensate(a, noah(60), s.now(), 2, math.ZeroInt())
		s.Require().NoError(err)
		g := s.grant(id)
		s.Require().Equal(math.NewInt(60), g.AwardValue, "valued at one reference unit a NOAH")
		s.Require().Equal(s.now()+types.MinCompensationPeriod, g.DueTime)
		s.Require().Equal([]types.GrantAction{types.GrantAction_GRANT_ACTION_CREATED}, s.actions(id))
		_, err = s.compensate(a, noah(41), s.now(), 1, math.ZeroInt())
		s.Require().ErrorContains(err, "insufficient uncommitted")
	})
	s.Run("a stablecoin award uses custody first and buys the rest within its cap", func() {
		s.SetupTest()
		s.fund(types.ContributorsName, noah(1_000), usd(60))
		_, err := s.compensate(a, usd(100), s.now(), 1, math.ZeroInt())
		s.Require().ErrorContains(err, "set max_offer")
		_, err = s.compensate(a, usd(100), s.now(), 1, math.NewInt(39))
		s.Require().ErrorIs(err, markettypes.ErrMaximumOfferExceeded)
		s.Require().Equal(sdk.NewCoins(noah(1_000), usd(60)), s.balances[types.ContributorsName], "a refused award buys nothing")

		id, err := s.compensate(a, usd(100), s.now(), 1, math.NewInt(40))
		s.Require().NoError(err)
		entries := s.journal(id)
		s.Require().Len(entries, 2)
		s.Require().Equal(types.GrantAction_GRANT_ACTION_CONVERTED, entries[1].Action)
		s.Require().Equal(usd(40), entries[1].Amount, "only the shortfall is bought")
		s.Require().Equal(math.NewInt(40), entries[1].Offer)
		s.Require().Equal(sdk.NewCoins(noah(960), usd(100)), s.balances[types.ContributorsName])
		res, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{Denom: "ausd"})
		s.Require().NoError(err)
		s.Require().Equal(usd(100), res.Accounts[0].Reserved)
		s.Require().True(res.Accounts[0].Uncommitted.IsZero())
		// Custody now covers the next award whole, so a cap is unused and nothing converts.
		s.fund(types.ContributorsName, usd(5))
		id, err = s.compensate(a, usd(5), s.now(), 1, math.OneInt())
		s.Require().NoError(err)
		s.Require().Equal([]types.GrantAction{types.GrantAction_GRANT_ACTION_CREATED}, s.actions(id))
	})
	s.Run("an offer the pool cannot cover is refused though the balance holds it", func() {
		s.SetupTest()
		s.fund(types.ContributorsName, noah(1_000))
		_, err := s.grantOwnership(a, 950, s.now())
		s.Require().NoError(err)
		_, err = s.compensate(a, usd(100), s.now(), 1, math.NewInt(100))
		s.Require().ErrorContains(err, "exceeds the contributor pool")
		s.Require().Equal(sdk.NewCoins(noah(1_000)), s.balances[types.ContributorsName])
	})
}

func (s *KeeperTestSuite) TestCancelGrants() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	a := testAddress(3)
	id := s.register(a)

	s.Require().ErrorContains(s.cancel(), "batch must contain")
	s.Require().ErrorContains(s.cancel(make([]uint64, types.MaxBatch+1)...), "batch must contain")
	s.Require().ErrorContains(s.cancel(0), "zero or duplicate")
	s.Require().ErrorContains(s.cancel(id, id), "zero or duplicate")
	err := s.cancel(id + 1)
	s.Require().ErrorIs(err, types.ErrGrantNotFound)
	s.requireCode(err, codes.NotFound)

	// Cancelled at once, the member keeps its paid tenth and the tranche reopens the rest.
	s.Require().NoError(s.cancel(id))
	g := s.grant(id)
	s.Require().True(g.Cancelled())
	s.Require().True(g.Remaining.IsZero())
	s.Require().Zero(g.DueTime)
	tranche, err := s.keeper.MemberTranche.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(9_000), tranche)
	entries := len(s.journal(id))
	// A closed grant cancels to nothing, so overlapping recovery proposals may share IDs.
	s.Require().NoError(s.cancel(id))
	s.Require().Len(s.journal(id), entries)
	s.Require().ErrorContains(s.suspend(a), "closed")
	s.Require().ErrorContains(s.reinstate(a), "closed")
}

func (s *KeeperTestSuite) TestSuspendAndReinstate() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	a, nobody := testAddress(3), testAddress(4)
	id := s.register(a)
	govReinstate := func(address string) error {
		return s.deliver(&types.MsgReinstateMembers{Authority: s.authority, Addresses: []string{address}})
	}

	err := s.suspend(nobody)
	s.Require().ErrorIs(err, types.ErrMemberNotFound)
	s.requireCode(err, codes.NotFound)
	err = govReinstate(nobody)
	s.Require().ErrorIs(err, types.ErrMemberNotFound)
	s.Require().ErrorContains(s.reinstate(a), "not suspended")
	s.Require().ErrorContains(govReinstate(a), "not suspended")
	s.Require().ErrorContains(s.suspend(), "batch must contain")
	s.Require().ErrorContains(s.suspend(a, a), "duplicate member in batch")

	s.Require().NoError(s.suspend(a))
	s.Require().Zero(s.grant(id).DueTime, "a suspended member leaves the queue")
	s.Require().Equal(s.now(), s.grant(id).SuspendedTime)
	s.Require().ErrorContains(s.suspend(a), "already suspended")
	s.Require().NoError(govReinstate(a))
	s.Require().Equal(s.now(), s.grant(id).DueTime, "reinstatement queues the arrears for this block")
	s.Require().NoError(s.suspend(a))
	s.Require().NoError(s.reinstate(a))
	s.Require().Equal([]types.GrantAction{
		types.GrantAction_GRANT_ACTION_CREATED, types.GrantAction_GRANT_ACTION_PAID,
		types.GrantAction_GRANT_ACTION_SUSPENDED, types.GrantAction_GRANT_ACTION_REINSTATED,
		types.GrantAction_GRANT_ACTION_SUSPENDED, types.GrantAction_GRANT_ACTION_REINSTATED,
	}, s.actions(id))
}

func (s *KeeperTestSuite) TestCommitteeRegistrar() {
	s.SetupTest()
	s.appointCommittee(0)
	committee, err := s.keeper.GrantsMandate.Get(s.ctx)
	s.Require().NoError(err)
	appoint := func(signer string, term uint64, registrar string, activation, expiry uint64) error {
		return s.deliver(&types.MsgCommitteeRegistrar{Committee: signer, ExpectedTerm: term, Registrar: registrar, ActivationHeight: activation, ExpiryHeight: expiry})
	}
	s.Require().ErrorContains(appoint(testAddress(9), s.term, s.registrar, committee.ActivationHeight, committee.ExpiryHeight), "exact appointed")
	s.Require().ErrorContains(appoint(s.committee, s.term+1, s.registrar, committee.ActivationHeight, committee.ExpiryHeight), "term mismatch")
	s.Require().ErrorContains(appoint(s.committee, s.term, s.authority, committee.ActivationHeight, committee.ExpiryHeight), "distinct from the disbursement authority")
	s.Require().ErrorContains(appoint(s.committee, s.term, s.registrar, committee.ActivationHeight-1, committee.ExpiryHeight), "inside the committee's")
	s.Require().ErrorContains(appoint(s.committee, s.term, s.registrar, committee.ActivationHeight, committee.ExpiryHeight+1), "inside the committee's")
	// A window inside the committee's that has already closed authorises nothing.
	s.setBlockHeight(int64(committee.ActivationHeight) + 1)
	s.Require().ErrorContains(appoint(s.committee, s.term, s.registrar, committee.ActivationHeight, committee.ActivationHeight+1), "not above the current height")

	s.Require().NoError(appoint(s.committee, s.term, s.registrar, committee.ActivationHeight, committee.ExpiryHeight))
	s.registrarTerm++
	s.openTranche(10_000)
	s.register(testAddress(3))
	// Disabling advances the registrar term and refuses the key at once.
	s.Require().NoError(appoint(s.committee, s.term, "", 0, 0))
	registrar, err := s.keeper.RegistrarMandate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(registrar.IsDisabled())
	s.Require().Equal(s.registrarTerm+1, registrar.Term)
	_, err = s.registerMembers(testAddress(4))
	s.Require().ErrorContains(err, "exact appointed")
}

func (s *KeeperTestSuite) TestRegisterMembers() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(25_000)
	a, b := testAddress(3), testAddress(4)
	registerAs := func(registrar string, term uint64, addresses ...string) error {
		return s.deliver(&types.MsgRegisterMembers{Registrar: registrar, ExpectedTerm: term, Addresses: addresses, Reference: "admission batch"})
	}

	_, err := s.registerMembers()
	s.Require().ErrorContains(err, "batch must contain")
	_, err = s.registerMembers(make([]string, types.MaxBatch+1)...)
	s.Require().ErrorContains(err, "batch must contain")
	_, err = s.registerMembers(a, a)
	s.Require().ErrorContains(err, "duplicate member in batch")
	_, err = s.registerMembers("nobody")
	s.Require().ErrorContains(err, "member is invalid")
	s.Require().ErrorContains(registerAs(a, s.registrarTerm, a), "exact appointed")
	s.Require().ErrorContains(registerAs(s.registrar, s.registrarTerm+1, a), "term mismatch")
	s.Require().ErrorContains(s.deliver(&types.MsgRegisterMembers{Registrar: s.registrar, ExpectedTerm: s.registrarTerm, Addresses: []string{a}}), "must not be empty")
	_, err = s.registerMembers(authtypes.NewModuleAddress(types.MembersName).String())
	s.Require().ErrorContains(err, "cannot receive funds")
	s.blocked[b] = true
	_, err = s.registerMembers(a, b)
	s.Require().ErrorContains(err, "cannot receive funds")
	s.blocked[b] = false
	_, err = s.registerMembers(a, b, testAddress(5))
	s.Require().ErrorContains(err, "exceeds the open member tranche")

	ids, err := s.registerMembers(a, b)
	s.Require().NoError(err)
	s.Require().Equal([]uint64{1, 2}, ids)
	g := s.grant(ids[0])
	s.Require().Equal(s.registrar, g.CreatedBy)
	s.Require().Equal(s.registrarTerm, g.MandateTerm)
	s.Require().Equal(math.NewInt(1_000), g.Paid, "a tenth at registration")
	s.Require().Equal(s.now()+11, g.DueTime, "queued for the boundary after the tenth")
	s.Require().Equal(sdk.NewCoins(noah(1_000)), s.received[a])
	entries := s.journal(ids[0])
	s.Require().Len(entries, 2)
	s.Require().Equal(s.registrar, entries[1].Actor, "the registrar makes the first payment")
	s.Require().Equal(a, entries[1].Subject)
	_, err = s.registerMembers(a)
	s.Require().ErrorContains(err, "already registered")
	// Nothing registers a cancelled member again either.
	s.Require().NoError(s.cancel(ids[0]))
	_, err = s.registerMembers(a)
	s.Require().ErrorContains(err, "already registered")
}

func (s *KeeperTestSuite) TestCommitteeCompensate() {
	s.SetupTest()
	s.appointCommittee(150)
	s.fund(types.ContributorsName, noah(1_000))
	a := testAddress(3)
	usage := func() math.Int {
		used, err := s.keeper.CompensationUsed.Get(s.ctx)
		s.Require().NoError(err)
		return used
	}
	s.Require().ErrorContains(s.deliver(&types.MsgCommitteeCompensate{Committee: testAddress(9), ExpectedTerm: s.term, Beneficiary: a, Amount: noah(1), StartTime: s.now(), Periods: 1, Reference: "x"}), "exact appointed")
	s.Require().ErrorContains(s.deliver(&types.MsgCommitteeCompensate{Committee: s.committee, ExpectedTerm: s.term + 1, Beneficiary: a, Amount: noah(1), StartTime: s.now(), Periods: 1, Reference: "x"}), "term mismatch")

	id, err := s.committeeCompensate(a, noah(100), 2)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(100), usage())
	s.Require().Equal(s.term, s.grant(id).MandateTerm)
	_, err = s.committeeCompensate(a, noah(51), 1)
	s.Require().ErrorContains(err, "term's compensation allowance")
	s.Require().Equal(math.NewInt(100), usage(), "a refused award charges nothing")
	// A cancel frees the principal and restores no allowance; a new term starts clean.
	s.Require().NoError(s.cancel(id))
	s.Require().Equal(math.NewInt(100), usage())
	s.appointCommittee(150)
	s.Require().True(usage().IsZero())
	_, err = s.committeeCompensate(a, noah(150), 1)
	s.Require().NoError(err)
}

func (s *KeeperTestSuite) TestSetPayee() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	s.fund(types.ContributorsName, noah(1_000))
	a, b, payee := testAddress(3), testAddress(4), testAddress(5)
	member := s.register(a)

	err := s.setPayee(a, member+1, payee)
	s.Require().ErrorIs(err, types.ErrGrantNotFound)
	s.requireCode(err, codes.NotFound)
	s.Require().ErrorContains(s.setPayee(b, member, payee), "only the member or contributor controller")
	s.Require().ErrorContains(s.setPayee(a, member, authtypes.NewModuleAddress(types.MembersName).String()), "cannot receive funds")
	s.blocked[payee] = true
	s.Require().ErrorContains(s.setPayee(a, member, payee), "cannot receive funds")
	s.blocked[payee] = false
	s.Require().ErrorContains(s.setPayee(a, member, a), "unchanged")
	s.Require().ErrorContains(s.setPayee(a, member, "nobody"), "payee is invalid")
	s.Require().NoError(s.setPayee(a, member, payee))
	s.Require().Equal(payee, s.grant(member).Payee)

	// A contributor controller rotation at the same address never reaches the member grant, and
	// the original key never reaches the award once control has moved.
	award, err := s.grantOwnership(a, 100, s.now())
	s.Require().NoError(err)
	s.Require().NoError(s.setController(a, a, b))
	s.Require().ErrorContains(s.setPayee(b, member, b), "only the member or contributor controller")
	s.Require().NoError(s.setPayee(a, member, b))
	s.Require().ErrorContains(s.setPayee(a, award, payee), "only the member or contributor controller")
	s.Require().NoError(s.setPayee(b, award, payee))

	// A grant paid in full has nothing left to redirect.
	s.advance(40)
	s.endBlock()
	s.Require().True(s.grant(member).Remaining.IsZero())
	s.Require().ErrorContains(s.setPayee(a, member, payee), "no remaining entitlement")
}

func (s *KeeperTestSuite) TestSetController() {
	s.SetupTest()
	a, b, c := testAddress(3), testAddress(4), testAddress(5)
	s.fund(types.ContributorsName, noah(1_000))

	err := s.setController(a, a, b)
	s.Require().ErrorIs(err, types.ErrBeneficiaryNotFound)
	s.requireCode(err, codes.NotFound)
	_, err = s.grantOwnership(a, 100, s.now())
	s.Require().NoError(err)
	s.Require().ErrorContains(s.setController(c, a, b), "only governance or the current controller")
	s.Require().ErrorContains(s.setController(a, a, "nobody"), "controller is invalid")
	s.Require().ErrorContains(s.setController(a, a, a), "unchanged")

	s.Require().NoError(s.setController(a, a, b))
	record, err := s.keeper.Beneficiaries.Get(s.ctx, sdk.MustAccAddressFromBech32(a))
	s.Require().NoError(err)
	s.Require().Equal(b, record.Controller)
	s.Require().ErrorContains(s.setController(a, a, c), "only governance or the current controller")
	// Governance recovers control; the journal leads from the address to the current controller.
	s.Require().NoError(s.setController(s.authority, a, c))
	res, err := s.queryServer.BeneficiaryJournal(s.ctx, &types.QueryBeneficiaryJournalRequest{Address: a})
	s.Require().NoError(err)
	s.Require().Len(res.Entries, 2)
	s.Require().Equal([]string{b, c}, []string{res.Entries[0].Controller, res.Entries[1].Controller})
	s.Require().Equal([]string{a, s.authority}, []string{res.Entries[0].Actor, res.Entries[1].Actor})
	s.Require().Equal(chain.NoahBaseDenom, s.grant(1).Amount.Denom)
}
