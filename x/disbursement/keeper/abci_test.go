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
