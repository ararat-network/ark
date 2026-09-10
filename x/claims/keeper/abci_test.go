package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/x/claims/types"
)

// submitGovernanceClaim records one pending governance claim of the supplied
// amount at the current height and returns its identifier.
func (s *KeeperTestSuite) submitGovernanceClaim(amount int64) uint64 {
	resp, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
		Authority: s.authority,
		Reference: "incident",
		Recipient: testAddress(3),
		Amount:    noahCoin(amount),
	})
	s.Require().NoError(err)
	return resp.ClaimId
}

func (s *KeeperTestSuite) TestEndBlockerSettlesDueClaims() {
	recipient := testAddress(3)

	s.Run("pays the stored recipient and releases the reservation", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)
		s.requireDueClaims(claimID)

		s.setBlockHeight(25)
		s.bankKeeper.EXPECT().
			SendCoinsFromModuleToAccount(
				gomock.Any(),
				types.InsuranceName,
				sdk.MustAccAddressFromBech32(recipient),
				sdk.NewCoins(noahCoin(100)),
			).
			Return(nil)

		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		claim, err := s.keeper.Claims.Get(s.ctx, claimID)
		s.Require().NoError(err)
		s.Require().Equal(types.ClaimStatus_CLAIM_STATUS_PAID, claim.Status)
		s.Require().Equal(uint64(25), claim.ClosingHeight)
		// Nobody signs for a settlement, so a paid claim names no canceller.
		s.Require().Empty(claim.CancelledBy)

		reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(reserved.IsZero())
		s.requireDueClaims()
	})

	s.Run("leaves a claim alone before its closing height", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)

		s.setBlockHeight(24)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_PENDING)
		s.requireDueClaims(claimID)
	})

	// The veto window closes at the closing height and the sweep runs after
	// that block's transactions, so the two can never act on the same claim.
	s.Run("settles the block the veto window closes", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)

		s.setBlockHeight(25)
		_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   claimID,
		})
		s.Require().ErrorContains(err, "cancellation period ended")

		s.expectPayouts(1)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_PAID)
	})

	s.Run("settles everything already overdue on one block", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.fundInsurance(5_000)

		s.setBlockHeight(20)
		first := s.submitGovernanceClaim(100)
		s.setBlockHeight(30)
		second := s.submitGovernanceClaim(100)

		// Height 40 is past both closing heights, so a single sweep drains
		// the backlog rather than one claim per block.
		s.setBlockHeight(40)
		s.expectPayouts(2)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		s.requireStatus(first, types.ClaimStatus_CLAIM_STATUS_PAID)
		s.requireStatus(second, types.ClaimStatus_CLAIM_STATUS_PAID)
		s.requireDueClaims()
	})

	s.Run("a cancelled claim is never settled", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)

		s.setBlockHeight(22)
		_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   claimID,
		})
		s.Require().NoError(err)

		// No payout is arranged: a Bank send here would fail the mock.
		s.setBlockHeight(25)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_CANCELLED)
	})

	s.Run("an empty queue is a no-op", func() {
		s.SetupTest()
		s.setBlockHeight(25)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	})
}

// TestEndBlockerSettlesWholeQueue proves the sweep is uncapped: every claim
// that has come due settles on the block it comes due, however many there are.
func (s *KeeperTestSuite) TestEndBlockerSettlesWholeQueue() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockHeight(20)
	s.fundInsurance(5_000)

	const total = 64
	claimIDs := make([]uint64, 0, total)
	for range total {
		claimIDs = append(claimIDs, s.submitGovernanceClaim(1))
	}

	s.setBlockHeight(25)
	s.expectPayouts(total)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	for _, claimID := range claimIDs {
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_PAID)
	}
	s.requireDueClaims()

	reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero())
}

// TestEndBlockerFailsUnpayableClaim proves an unpayable claim ends failed
// rather than halting the block, staying pending, or silently vanishing — and
// that its reservation is released, since a claim that will never be paid must
// stop encumbering Insurance.
func (s *KeeperTestSuite) TestEndBlockerFailsUncoveredClaim() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockHeight(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)
	other := s.submitGovernanceClaim(100)

	// Coverage is one of the two payability tests. It cannot go short while
	// Claims is the only Insurance outflow, so it is forced here. It fails both
	// claims: neither is payable at 50.
	s.setBlockHeight(25)
	s.fundInsurance(50)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_FAILED)
	// One unpayable claim does not stop the sweep reaching the next.
	s.requireStatus(other, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	failed, err := s.keeper.Claims.Get(s.ctx, claimID)
	s.Require().NoError(err)
	s.Require().Equal(uint64(25), failed.ClosingHeight)
	s.Require().Empty(failed.CancelledBy)

	reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")

	// Terminal on the first attempt: a later block with a funded account does
	// not resurrect it.
	s.setBlockHeight(26)
	s.fundInsurance(5_000)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_FAILED)
}

// TestEndBlockerFailsBlockedRecipient proves settlement re-tests the recipient
// rather than trusting the check submission made. Blocking is decided by app
// wiring, so it is the one payability input a claim's own stored state cannot
// settle on its own.
func (s *KeeperTestSuite) TestEndBlockerFailsBlockedRecipient() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockHeight(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)

	// The recipient was payable at submission and is blocked before the claim
	// comes due. No expectPayouts: Bank must never be asked to pay it.
	s.blockedAddrs[testAddress(3)] = struct{}{}
	s.setBlockHeight(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")
}

// TestEndBlockerFailsInsuranceSelfPayment checks Claim.Validate rejects Insurance as its own
// recipient. Bank permits this address and denom, so only record validation prevents a false
// payment with unchanged custody.
func (s *KeeperTestSuite) TestEndBlockerFailsInsuranceSelfPayment() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockHeight(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)

	// Submission refuses this recipient, so a stored claim can only carry it if
	// the rules moved under an immutable record. Written directly, the way such
	// a record would arrive. The closing height and ID are untouched, so the
	// queue entry stays valid and the claim still comes due.
	claim, err := s.keeper.Claims.Get(s.ctx, claimID)
	s.Require().NoError(err)
	claim.Recipient = authtypes.NewModuleAddress(types.InsuranceName).String()
	s.Require().NoError(s.keeper.Claims.Set(s.ctx, claimID, claim))

	// No expectPayouts: Bank must never be asked to pay it.
	s.setBlockHeight(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")
}

// TestEndBlockerDropsDivergentQueueEntry proves a queue entry that disagrees
// with its claim is discarded without touching the record. Marking it failed
// would overwrite the status of a claim that was already settled.
func (s *KeeperTestSuite) TestEndBlockerDropsDivergentQueueEntry() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockHeight(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)

	s.setBlockHeight(22)
	_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Authority: s.authority,
		ClaimId:   claimID,
	})
	s.Require().NoError(err)

	// Re-queue the cancelled claim by hand, which is the shape any index/record
	// divergence would take. No payout is arranged: a Bank send here fails the
	// mock.
	s.Require().NoError(s.keeper.DueClaims.Set(s.ctx, collections.Join(uint64(25), claimID)))

	s.setBlockHeight(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_CANCELLED)
	s.requireDueClaims()
}

// queueClaimBypassingSubmission writes a pending claim and its settlement index
// entry directly, the shape a record takes when the rules that once admitted it
// move under a stored claim across a binary upgrade. Submission would refuse
// it today, which is exactly why the sweep must not trust that refusal ever ran.
func (s *KeeperTestSuite) queueClaimBypassingSubmission(claim types.Claim) {
	s.Require().NoError(s.keeper.Claims.Set(s.ctx, claim.ClaimId, claim))
	s.Require().NoError(s.keeper.DueClaims.Set(s.ctx, collections.Join(claim.ClosingHeight, claim.ClaimId)))
	s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, claim.Amount.Amount))
}

// TestEndBlockerFailsUnparseableRecipient proves a stored recipient the current
// binary can no longer parse fails its claim instead of the block. Submission
// proved the string parsed, but parse rules ride with the binary exactly as the
// blocked set does, and a claim can sit pending across an upgrade.
func (s *KeeperTestSuite) TestEndBlockerFailsUnparseableRecipient() {
	s.SetupTest()
	s.fundInsurance(5_000)
	s.queueClaimBypassingSubmission(types.Claim{
		ClaimId:         1,
		Submitter:       testAddress(1),
		Origin:          types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE,
		Reference:       "incident",
		Recipient:       "not-an-address",
		Amount:          noahCoin(100),
		Status:          types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight: 20,
		ClosingHeight:   25,
	})

	// No payout is arranged: Bank must never be asked to pay it.
	s.setBlockHeight(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(1, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")
}

// TestEndBlockerFailsRecordFailingValidation proves a record that stops passing
// the current binary's validation rules fails instead of halting the block: the
// payability checks can all pass while some other stored fact has drifted out
// from under the rules that admitted it.
func (s *KeeperTestSuite) TestEndBlockerFailsRecordFailingValidation() {
	s.SetupTest()
	s.fundInsurance(5_000)
	s.queueClaimBypassingSubmission(types.Claim{
		ClaimId:         1,
		Submitter:       testAddress(1),
		Origin:          types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE,
		Reference:       "   ", // fails validation while the recipient still parses
		Recipient:       testAddress(3),
		Amount:          noahCoin(100),
		Status:          types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight: 20,
		ClosingHeight:   25,
	})

	// No payout is arranged: Bank must never be asked to pay it.
	s.setBlockHeight(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(1, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")
}
