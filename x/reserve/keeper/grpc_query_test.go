package keeper_test

import (
	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"github.com/ararat-network/ark/x/reserve/types"
)

// closePosition closes one position as the committee.
func (s *KeeperTestSuite) closePosition(committee string, positionID uint64) {
	_, err := s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
	})
	s.Require().NoError(err)
}

// openAndClosed deploys three positions and closes the middle one, so the open
// set is discontiguous in identifier order. A query that merely trimmed a
// prefix or suffix of the record would still pass on a contiguous fixture.
func (s *KeeperTestSuite) openAndClosed(committee, destination string) (open []uint64, closed uint64) {
	s.appointCommittee(committee, 10_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(10_000)

	first := s.deploy(committee, destination, 300, 3, 0)
	second := s.deploy(committee, destination, 200, 2, 0)
	third := s.deploy(committee, destination, 100, 1, 0)
	s.closePosition(committee, second)

	return []uint64{first, third}, second
}

func (s *KeeperTestSuite) positionIDs(positions []types.Position) []uint64 {
	ids := make([]uint64, 0, len(positions))
	for _, position := range positions {
		ids = append(ids, position.PositionId)
	}
	return ids
}

// TestPositionQueriesPartitionByStatus pins that each query walks one store,
// so neither answer is the other one trimmed. The fixture closes the middle
// position, so a query that merely dropped a prefix or suffix of the record
// would still be caught.
func (s *KeeperTestSuite) TestPositionQueriesPartitionByStatus() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.Run("each query answers only its own store", func() {
		s.SetupTest()
		open, closed := s.openAndClosed(committee, destination)

		openResp, err := s.queryServer.OpenPositions(s.ctx, &types.QueryOpenPositionsRequest{})
		s.Require().NoError(err)
		s.Require().Equal(open, s.positionIDs(openResp.Positions))
		for _, position := range openResp.Positions {
			s.Require().False(position.IsClosed())
		}

		closedResp, err := s.queryServer.ClosedPositions(s.ctx, &types.QueryClosedPositionsRequest{})
		s.Require().NoError(err)
		s.Require().Equal([]uint64{closed}, s.positionIDs(closedResp.Positions))
		s.Require().True(closedResp.Positions[0].IsClosed())
	})

	// Closing moves the record out of the open store; open-set walks require no status predicate.
	s.Run("closing removes the position from the open store", func() {
		s.SetupTest()
		_, closed := s.openAndClosed(committee, destination)

		held, err := s.keeper.OpenPositions.Has(s.ctx, closed)
		s.Require().NoError(err)
		s.Require().False(held)
		held, err = s.keeper.ClosedPositions.Has(s.ctx, closed)
		s.Require().NoError(err)
		s.Require().True(held)
	})

	// A page of one must return one: the closed position is in another store
	// and cannot consume a page here.
	s.Run("a page fills with open positions", func() {
		s.SetupTest()
		open, _ := s.openAndClosed(committee, destination)

		var seen []uint64
		var key []byte
		for range open {
			resp, err := s.queryServer.OpenPositions(s.ctx, &types.QueryOpenPositionsRequest{
				Pagination: &sdkquery.PageRequest{Limit: 1, Key: key},
			})
			s.Require().NoError(err)
			s.Require().Len(resp.Positions, 1)
			seen = append(seen, resp.Positions[0].PositionId)
			key = resp.Pagination.NextKey
		}
		s.Require().Equal(open, seen)
	})

	s.Run("an emptied open set is not an error", func() {
		s.SetupTest()
		open, closed := s.openAndClosed(committee, destination)
		for _, positionID := range open {
			s.closePosition(committee, positionID)
		}

		openResp, err := s.queryServer.OpenPositions(s.ctx, &types.QueryOpenPositionsRequest{})
		s.Require().NoError(err)
		s.Require().Empty(openResp.Positions)

		// Everything landed in the closed store, still keyed by the identifiers
		// it was opened under.
		closedResp, err := s.queryServer.ClosedPositions(s.ctx, &types.QueryClosedPositionsRequest{})
		s.Require().NoError(err)
		s.Require().Equal([]uint64{open[0], closed, open[1]}, s.positionIDs(closedResp.Positions))
	})

	// Offsets and totals read every entry skipped or counted, so both queries refuse them.
	s.Run("both queries refuse offsets and totals", func() {
		s.SetupTest()
		s.openAndClosed(committee, destination)

		for _, page := range []*sdkquery.PageRequest{{Offset: 1}, {CountTotal: true}} {
			_, err := s.queryServer.OpenPositions(s.ctx, &types.QueryOpenPositionsRequest{Pagination: page})
			s.Require().ErrorContains(err, "use key pagination")
			_, err = s.queryServer.ClosedPositions(s.ctx, &types.QueryClosedPositionsRequest{Pagination: page})
			s.Require().ErrorContains(err, "use key pagination")
		}
	})
}

// TestPositionQueryResolvesEitherStore pins the by-ID lookup across the split:
// a caller holding an identifier should not have to know whether it has closed,
// which is the one place the partition must stay invisible.
func (s *KeeperTestSuite) TestPositionQueryResolvesEitherStore() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	open, closed := s.openAndClosed(committee, destination)

	openResp, err := s.queryServer.Position(s.ctx, &types.QueryPositionRequest{PositionId: open[0]})
	s.Require().NoError(err)
	s.Require().Equal(open[0], openResp.Position.PositionId)
	s.Require().False(openResp.Position.IsClosed())

	closedResp, err := s.queryServer.Position(s.ctx, &types.QueryPositionRequest{PositionId: closed})
	s.Require().NoError(err)
	s.Require().Equal(closed, closedResp.Position.PositionId)
	s.Require().True(closedResp.Position.IsClosed())

	_, err = s.queryServer.Position(s.ctx, &types.QueryPositionRequest{PositionId: 999})
	s.Require().ErrorContains(err, "not found")
}

// TestBalanceReportsOutstandingDeployment pins the figure that stayed in the
// Balance response: unlike a count, no page request produces it.
func (s *KeeperTestSuite) TestBalanceReportsOutstandingDeployment() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.openAndClosed(committee, destination)
	s.fundReserve(9_400)

	resp, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
	s.Require().NoError(err)
	s.Require().Equal(noahCoin(9_400), resp.Balance)
	// 300 + 100 open; the closed 200 is out.
	s.Require().Equal(noahCoin(400), resp.OutstandingDeployed)
}

// pageLedger walks the Ledger query a page of one at a time, following each next key.
func (s *KeeperTestSuite) pageLedger(positionID uint64) []types.AccountingEntry {
	entries := make([]types.AccountingEntry, 0)
	var key []byte
	for {
		resp, err := s.queryServer.Ledger(s.ctx, &types.QueryLedgerRequest{
			PositionId: positionID,
			Pagination: &sdkquery.PageRequest{Limit: 1, Key: key},
		})
		s.Require().NoError(err)
		entries = append(entries, resp.Ledger...)
		if len(resp.Pagination.NextKey) == 0 {
			return entries
		}
		key = resp.Pagination.NextKey
	}
}

// TestLedgerQueryPagesByPosition pins that a position filter pages through that
// position's own entries, interleaved with another's, and that import rebuilds
// the index it reads.
func (s *KeeperTestSuite) TestLedgerQueryPagesByPosition() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 10_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(10_000)
	first := s.deploy(committee, destination, 300, 3, 0)
	second := s.deploy(committee, destination, 200, 2, 0)
	s.deploy(committee, destination, 100, 1, first)
	s.closePosition(committee, second)

	check := func() {
		s.Require().Len(s.ledgerFor(first), 2)
		s.Require().Equal(s.ledgerFor(first), s.pageLedger(first))
		s.Require().Equal(s.ledgerFor(second), s.pageLedger(second))
		s.Require().Equal(s.wholeLedger(), s.pageLedger(0))
		s.Require().Empty(s.pageLedger(second + 1))
	}
	check()

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.SetupTest()
	s.fundReserve(10_000)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))
	check()
}
