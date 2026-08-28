package keeper_test

import (
	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

const (
	// attributedRecovery is what attributedPosition books, and what a reversal
	// must walk back off the position.
	attributedRecovery = 250
	// reversalEvidence stands for the custodian statement contradicting an
	// attribution.
	reversalEvidence = "custodian-statement-0x09"
	inflowEvidence   = "tx-0x02"
	// anyEvidence serves the cases refused before the reference is read.
	anyEvidence = "memo"
)

// attributedPosition opens a position and attributes one NOAH return to it,
// returning the position and the attribution a reversal will name.
func (s *KeeperTestSuite) attributedPosition(committee, destination string) (uint64, uint64) {
	positionID := s.deploy(committee, destination, 400, 4, 0)
	resp, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		ReturnedCoin:      noahCoin(attributedRecovery),
		RemainingQuantity: assetCoin(2),
		Reference:         inflowEvidence,
	})
	s.Require().NoError(err)
	return positionID, resp.EntryId
}

// TestCommitteeReverseReturn covers the remedy for the one recorded movement
// whose linkage the chain cannot verify: Bank witnesses that coins arrived,
// never which position they settle.
func (s *KeeperTestSuite) TestCommitteeReverseReturn() {
	committee := testAddress(2)
	destination := testAddress(3)

	setup := func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
	}

	s.Run("reverses the recovery and records what it undid", func() {
		setup()
		positionID, attributionID := s.attributedPosition(committee, destination)

		before, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(attributedRecovery), before.Returned)

		resp, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses:  attributionID,
			Reference: reversalEvidence,
		})
		s.Require().NoError(err)

		// The cost basis is untouched: a reversal retracts the leg the committee
		// asserted, never the one Bank executed.
		after, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), after.Returned)
		s.Require().Equal(before.Deployed, after.Deployed)
		s.Require().Equal(before.Quantity, after.Quantity)

		// The mistake stays readable beside its reversal.
		entries := s.ledgerFor(positionID)
		reversal := entries[len(entries)-1]
		s.Require().Equal(resp.EntryId, reversal.EntryId)
		s.Require().Equal(types.EntryKind_ENTRY_KIND_RETURN_REVERSAL, reversal.Kind)
		s.Require().Equal(attributionID, reversal.Corrects)
		s.Require().Equal(noahCoin(attributedRecovery), reversal.MovedNoahValue)
		s.Require().Equal(uint64(1), reversal.Term)

		attribution, err := s.keeper.Ledger.Get(s.ctx, attributionID)
		s.Require().NoError(err)
		s.Require().Equal(types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION, attribution.Kind)
		s.Require().Equal(noahCoin(attributedRecovery), attribution.MovedNoahValue)
	})

	// The point of the fixed value: a committee must not reverse across a rate
	// move and book the difference.
	s.Run("reverses at the attributed value, not the current one", func() {
		setup()
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 4)
		rates := oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")}
		s.stubRates(rates)

		positionID := s.deploy(committee, destination, 400, 4, 0)
		resp, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      paperCoin(4),
			RemainingQuantity: assetCoin(0),
			Reference:         "tx-0x03",
		})
		s.Require().NoError(err)

		booked, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(80), booked.Returned)

		// A re-priced reversal would leave 40 anoah of phantom recovery.
		rates[chain.USDBaseDenom] = math.LegacyMustNewDecFromStr("0.1")

		_, err = s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses:  resp.EntryId,
			Reference: reversalEvidence,
		})
		s.Require().NoError(err)

		after, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), after.Returned)

		entries := s.ledgerFor(positionID)
		reversal := entries[len(entries)-1]
		s.Require().Equal(noahCoin(80), reversal.MovedNoahValue)
		s.Require().Equal(paperCoin(4), reversal.MovedCoin)
	})

	s.Run("refuses a second reversal of one attribution", func() {
		setup()
		positionID, attributionID := s.attributedPosition(committee, destination)

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses: attributionID, Reference: reversalEvidence,
		})
		s.Require().NoError(err)

		ledgerBefore := len(s.wholeLedger())
		_, err = s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses: attributionID, Reference: "custodian-statement-0x0a",
		})
		s.Require().ErrorContains(err, "has already been reversed")

		// Nothing was written, so replaying cannot walk the recovery below zero.
		after, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), after.Returned)
		s.Require().Len(s.wholeLedger(), ledgerBefore)
	})

	// Only an attribution adds to the recovery, so only one can be taken out.
	// A deployment names the same position and passes every other check.
	s.Run("refuses reversing anything but an attribution", func() {
		setup()
		positionID, _ := s.attributedPosition(committee, destination)
		deploymentEntry := s.ledgerFor(positionID)[0]
		s.Require().Equal(types.EntryKind_ENTRY_KIND_DEPLOYMENT, deploymentEntry.Kind)

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses: deploymentEntry.EntryId, Reference: anyEvidence,
		})
		s.Require().ErrorContains(err, "only a return attribution can be reversed")
	})

	s.Run("refuses an entry belonging to another position", func() {
		setup()
		first, firstAttribution := s.attributedPosition(committee, destination)
		second := s.deploy(committee, destination, 100, 1, 0)
		s.Require().NotEqual(first, second)

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: second,
			Reverses: firstAttribution, Reference: anyEvidence,
		})
		s.Require().ErrorContains(err, "belongs to position 1, not 2")

		// The position that did hold the recovery keeps it.
		position, err := s.keeper.OpenPositions.Get(s.ctx, first)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(attributedRecovery), position.Returned)
	})

	s.Run("refuses an unknown entry", func() {
		setup()
		positionID, _ := s.attributedPosition(committee, destination)

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses: 999, Reference: anyEvidence,
		})
		s.Require().ErrorContains(err, "getting reversed entry 999")
	})

	s.Run("refuses a stale term and an unappointed signer", func() {
		setup()
		positionID, attributionID := s.attributedPosition(committee, destination)

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 2, PositionId: positionID,
			Reverses: attributionID, Reference: anyEvidence,
		})
		s.Require().ErrorContains(err, "term mismatch")

		_, err = s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: testAddress(9), ExpectedTerm: 1, PositionId: positionID,
			Reverses: attributionID, Reference: anyEvidence,
		})
		s.Require().ErrorContains(err, "not the exact appointed committee")
	})

	// A reversal contradicts a recorded movement, so it names its evidence.
	s.Run("refuses a reversal without evidence", func() {
		setup()
		positionID, attributionID := s.attributedPosition(committee, destination)

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses: attributionID,
		})
		s.Require().ErrorContains(err, "entry reference must not be empty")
	})

	// The repair the mechanism exists for: the attribution named the wrong
	// position, and the coins are still in the account.
	s.Run("frees the inflow to be re-attributed", func() {
		setup()
		first, firstAttribution := s.attributedPosition(committee, destination)
		second := s.deploy(committee, destination, 100, 1, 0)

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: first,
			Reverses: firstAttribution, Reference: reversalEvidence,
		})
		s.Require().NoError(err)

		_, err = s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: second,
			ReturnedCoin:      noahCoin(attributedRecovery),
			RemainingQuantity: assetCoin(1),
			Reference:         inflowEvidence,
		})
		s.Require().NoError(err)

		wrong, err := s.keeper.OpenPositions.Get(s.ctx, first)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), wrong.Returned)
		right, err := s.keeper.OpenPositions.Get(s.ctx, second)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(attributedRecovery), right.Returned)
	})
}

// TestReverseReturnGovernance covers the authority half: the same power on the
// same terms, held while no mandate is live — the deadlock the governance
// closure path already exists to avoid.
func (s *KeeperTestSuite) TestReverseReturnGovernance() {
	committee := testAddress(2)
	destination := testAddress(3)

	s.Run("reverses as governance under a revoked mandate", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID, attributionID := s.attributedPosition(committee, destination)

		// The appointment is revoked, so no committee message can be authorised.
		s.Require().NoError(s.keeper.Mandate.Set(s.ctx, types.NewDisabledReserveMandate(2)))

		_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reverses: attributionID, Reference: anyEvidence,
		})
		s.Require().Error(err)

		resp, err := s.msgServer.ReverseReturn(s.ctx, &types.MsgReverseReturn{
			Authority:  s.authority,
			PositionId: positionID,
			Reverses:   attributionID,
			Reference:  reversalEvidence,
		})
		s.Require().NoError(err)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), position.Returned)

		// Term zero records the standing authority rather than an appointment.
		reversal, err := s.keeper.Ledger.Get(s.ctx, resp.EntryId)
		s.Require().NoError(err)
		s.Require().Equal(uint64(0), reversal.Term)
		s.Require().Equal(s.authority, reversal.RecordedBy)
	})

	s.Run("refuses a signer that is not the authority", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID, attributionID := s.attributedPosition(committee, destination)

		_, err := s.msgServer.ReverseReturn(s.ctx, &types.MsgReverseReturn{
			Authority:  testAddress(9),
			PositionId: positionID,
			Reverses:   attributionID,
			Reference:  anyEvidence,
		})
		s.Require().Error(err)
	})

	// A closed position's recovery should read as true as an open one's, and is
	// where a custodian's contradiction is most likely to arrive.
	s.Run("reverses a closed position's attribution", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID, attributionID := s.attributedPosition(committee, destination)

		_, err := s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reference: "closure-memo",
		})
		s.Require().NoError(err)

		closedBefore, err := s.keeper.ClosedPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(attributedRecovery), closedBefore.Returned)
		s.Require().Equal(math.NewInt(-150), closedBefore.Realised())

		_, err = s.msgServer.ReverseReturn(s.ctx, &types.MsgReverseReturn{
			Authority:  s.authority,
			PositionId: positionID,
			Reverses:   attributionID,
			Reference:  reversalEvidence,
		})
		s.Require().NoError(err)

		// The record goes back where it came from: a reversal cannot reopen.
		open, err := s.keeper.OpenPositions.Has(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().False(open)
		after, err := s.keeper.ClosedPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), after.Returned)
		s.Require().Equal(math.NewInt(-400), after.Realised())
		s.Require().Equal(closedBefore.ClosedHeight, after.ClosedHeight)
	})
}

// TestReverseReturnGenesisRoundTrip pins that the reversal and its derived
// guard survive export and reimport, so an imported chain refuses a replay the
// exporting chain would have refused.
func (s *KeeperTestSuite) TestReverseReturnGenesisRoundTrip() {
	committee := testAddress(2)
	destination := testAddress(3)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)
	positionID, attributionID := s.attributedPosition(committee, destination)

	_, err := s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		Reverses: attributionID, Reference: reversalEvidence,
	})
	s.Require().NoError(err)

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(exported.Validate())

	s.SetupTest()
	s.fundReserve(1_000)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))
	s.setBlockHeight(20)

	imported, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
	s.Require().NoError(err)
	s.Require().Equal(noahCoin(0), imported.Returned)

	// Rebuilt from the ledger, so the attribution is still reversed.
	_, err = s.msgServer.CommitteeReverseReturn(s.ctx, &types.MsgCommitteeReverseReturn{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		Reverses: attributionID, Reference: "custodian-statement-0x0a",
	})
	s.Require().ErrorContains(err, "has already been reversed")

	reexported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(reexported.Validate())
	s.Require().Equal(exported.Ledger, reexported.Ledger)
}
