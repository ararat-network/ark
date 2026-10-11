package keeper_test

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/disbursement/types"
)

// TestEndBlockerLooksWithoutPaying pins a look that finds nothing accrued: a member reinstated
// inside the period it was suspended in is queued for the block, which pays nothing, journals
// nothing, and moves the grant on to its next boundary.
func (s *KeeperTestSuite) TestEndBlockerLooksWithoutPaying() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	a := testAddress(3)
	id := s.register(a)
	s.advance(int64(hold))
	s.endBlock()
	s.Require().Equal(math.NewInt(1_000), s.grant(id).Paid, "the tenth once the hold ends")
	s.Require().NoError(s.suspend(a))
	s.Require().NoError(s.reinstate(a))
	s.Require().Equal(s.now(), s.grant(id).DueTime)
	entries := len(s.journal(id))

	s.endBlock()
	g := s.grant(id)
	s.Require().Equal(math.NewInt(1_000), g.Paid)
	s.Require().Equal(s.now()+10, g.DueTime, "waiting for the boundary after the tenth")
	s.Require().Len(s.journal(id), entries)
	s.Require().Empty(s.deferrals())
	s.Require().Equal(sdk.NewCoins(noah(1_000)), s.received[a])
}

// TestEndBlockerDefersARefusedPayment pins the one send the block does not halt on: a payee Bank
// refuses waits the sooner of its next boundary and a retry interval, the deferral is published and
// nothing journaled, and a corrected payee is looked at in its own block.
func (s *KeeperTestSuite) TestEndBlockerDefersARefusedPayment() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	a, payee := testAddress(3), testAddress(4)
	id := s.register(a)
	s.refused[a] = errRefused
	s.advance(int64(hold) + 10)
	s.endBlock()
	g := s.grant(id)
	s.Require().True(g.Paid.IsZero(), "the refused send moved nothing")
	s.Require().Equal(s.now()+10, g.DueTime, "the next boundary comes before a retry interval")
	deferred := s.deferrals()
	s.Require().Len(deferred, 1)
	s.Require().Equal(id, deferred[0].GrantId)
	s.Require().Equal(a, deferred[0].Payee)
	s.Require().Equal(math.NewInt(4_000), deferred[0].Amount, "the tenth and the first month")
	s.Require().Equal(g.DueTime, deferred[0].DueTime)
	s.Require().Len(s.journal(id), 1)
	// Past its schedule, the grant waits a whole retry interval between looks.
	s.advance(30)
	s.endBlock()
	s.Require().Equal(s.now()+types.PaymentRetryInterval, s.grant(id).DueTime)
	s.Require().Len(s.deferrals(), 2)

	s.Require().NoError(s.setPayee(a, id, payee))
	s.Require().Equal(s.now(), s.grant(id).DueTime, "a corrected destination is looked at now")
	s.endBlock()
	s.Require().Equal(sdk.NewCoins(noah(10_000)), s.received[payee])
	s.Require().Zero(s.grant(id).DueTime)
	s.Require().Len(s.deferrals(), 2)
}

// TestEndBlockerDropsAStrayDueKey pins that a queue key its grant does not carry is dropped rather
// than paid, and that the grant keeps its own.
func (s *KeeperTestSuite) TestEndBlockerDropsAStrayDueKey() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	id := s.register(testAddress(3))
	s.Require().NoError(s.keeper.Due.Set(s.ctx, collections.Join(s.now(), id)))
	s.endBlock()
	s.Require().True(s.grant(id).Paid.IsZero(), "nothing is due inside the hold")
	stray, err := s.keeper.Due.Has(s.ctx, collections.Join(s.now(), id))
	s.Require().NoError(err)
	s.Require().False(stray)
	own, err := s.keeper.Due.Has(s.ctx, collections.Join(s.grant(id).DueTime, id))
	s.Require().NoError(err)
	s.Require().True(own)
}

// TestEndBlockerPaysOldestDueFirst pins the order the per-block bound cuts in: due time, then grant
// ID. The last ten awards fall due first, so a queue walked by ID or newest first pays the wrong
// thousand.
func (s *KeeperTestSuite) TestEndBlockerPaysOldestDueFirst() {
	s.SetupTest()
	const later, earlier = types.MaxPaymentsPerBlock, 10
	a := testAddress(3)
	s.fund(types.ContributorsName, noah(4*(later+earlier)))
	start := s.now()
	for range later {
		_, err := s.grantOwnership(a, 4, start+10)
		s.Require().NoError(err)
	}
	for range earlier {
		_, err := s.grantOwnership(a, 4, start)
		s.Require().NoError(err)
	}
	s.advance(int64(hold) + 10)
	s.endBlock()
	for id := uint64(1); id <= later+earlier; id++ {
		g := s.grant(id)
		switch {
		case id > later:
			s.Require().Equal(math.NewInt(4), g.Paid, "grant %d, due first, is paid whole", id)
		case id <= later-earlier:
			s.Require().Equal(math.NewInt(1), g.Paid, "grant %d is paid its first period", id)
		default:
			s.Require().True(g.Paid.IsZero(), "grant %d is past the bound", id)
			s.Require().Equal(s.now(), g.DueTime, "grant %d stays due for the next block", id)
		}
	}
}

// TestEndBlockerFailsOnACustodyShortfall pins that custody below the book fails the block: a
// shortfall is broken accounting, never a payee's refusal to defer.
func (s *KeeperTestSuite) TestEndBlockerFailsOnACustodyShortfall() {
	s.SetupTest()
	s.appointCommittee(0)
	s.appointRegistrar()
	s.openTranche(10_000)
	id := s.register(testAddress(3))
	s.balances[types.MembersName] = sdk.NewCoins(noah(999))
	s.advance(int64(hold))
	err := s.keeper.EndBlocker(s.ctx)
	s.Require().ErrorContains(err, "disbursement_members custody is below the 1000anoah payment")
	s.Require().NotErrorIs(err, types.ErrPaymentRefused)
	s.Require().Empty(s.deferrals())
	s.Require().True(s.grant(id).Paid.IsZero())
}
