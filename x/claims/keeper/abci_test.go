package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

func (s *KeeperTestSuite) TestEndBlockerSettlesDueClaims() {
	recipient := testAddress(3)

	s.Run("pays the stored recipient and releases the reservation", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockTime(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)
		s.requireDueClaims(claimID)

		s.setBlockTime(25)
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
		s.Require().Equal(uint64(25), claim.ClosingTime)
		// Nobody signs for a settlement, so a paid claim names no canceller.
		s.Require().Empty(claim.CancelledBy)

		reserved, err := s.reserved(chain.NoahBaseDenom)
		s.Require().NoError(err)
		s.Require().True(reserved.IsZero())
		s.requireDueClaims()
	})

	s.Run("leaves a claim alone before its closing time", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockTime(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)

		s.setBlockTime(24)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_PENDING)
		s.requireDueClaims(claimID)
	})

	// The veto window closes at the closing time and the sweep runs after
	// that block's transactions, so the two can never act on the same claim.
	s.Run("settles the block the veto window closes", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockTime(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)

		s.setBlockTime(25)
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

		s.setBlockTime(20)
		first := s.submitGovernanceClaim(100)
		s.setBlockTime(30)
		second := s.submitGovernanceClaim(100)

		// Time 40 is past both closing times, so a single sweep drains
		// the backlog rather than one claim per block.
		s.setBlockTime(40)
		s.expectPayouts(2)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))

		s.requireStatus(first, types.ClaimStatus_CLAIM_STATUS_PAID)
		s.requireStatus(second, types.ClaimStatus_CLAIM_STATUS_PAID)
		s.requireDueClaims()
	})

	s.Run("a cancelled claim is never settled", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockTime(20)
		s.fundInsurance(5_000)
		claimID := s.submitGovernanceClaim(100)

		s.setBlockTime(22)
		_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   claimID,
		})
		s.Require().NoError(err)

		// No payout is arranged: a Bank send here would fail the mock.
		s.setBlockTime(25)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_CANCELLED)
	})

	s.Run("an empty queue is a no-op", func() {
		s.SetupTest()
		s.setBlockTime(25)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	})
}

// TestWindowsRunOnBlockTime pins every Claims window to block time: one closes when the block time
// reaches it, however little the height moves, and never because the height moved.
func (s *KeeperTestSuite) TestWindowsRunOnBlockTime() {
	// Submitted at a block time past every height the suite reaches, so a window read off the
	// height stays open after the block time closes it, and the height can jump past it alone.
	const day, submitted = 86_400, 10 * heightOrigin
	committee := testAddress(1)
	submit := func() uint64 {
		s.setCancellationPeriod(day)
		s.setBlockTime(submitted)
		s.fundInsurance(5_000)
		return s.submitGovernanceClaim(100)
	}
	cancel := func(claimID uint64) error {
		_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{Authority: s.authority, ClaimId: claimID})
		return err
	}
	committeeSubmit := func() error {
		_, err := s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
			Committee: committee, ExpectedTerm: 1, Reference: "incident", Recipient: testAddress(3), Amount: noahCoin(100),
		})
		return err
	}

	s.Run("a claim settles in the next block once its closing time arrives", func() {
		s.SetupTest()
		claimID := submit()
		s.setBlockTime(submitted + day)
		s.expectPayouts(1)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_PAID)
	})

	s.Run("a claim stays pending while only the height moves", func() {
		s.SetupTest()
		claimID := submit()
		// No payout is arranged: a Bank send here would fail the mock.
		s.setBlockHeight(submitted + 10*day)
		s.Require().NoError(s.keeper.EndBlocker(s.ctx))
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_PENDING)
		s.requireDueClaims(claimID)
	})

	s.Run("the veto window closes in the next block once its closing time arrives", func() {
		s.SetupTest()
		claimID := submit()
		s.setBlockTime(submitted + day)
		s.Require().ErrorContains(cancel(claimID), "cancellation period ended")
	})

	s.Run("the veto window stays open while only the height moves", func() {
		s.SetupTest()
		claimID := submit()
		s.setBlockHeight(submitted + 10*day)
		s.Require().NoError(cancel(claimID))
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_CANCELLED)
	})

	s.Run("the committee window closes in the next block at its expiry time", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee)
		s.fundInsurance(5_000)
		s.setBlockTime(95)
		s.Require().NoError(committeeSubmit())
		s.setBlockTime(100)
		s.Require().ErrorContains(committeeSubmit(), "Claims mandate: mandate is not active")
	})

	// The window spans the suite's heights, not its block time.
	s.Run("a committee window the height sits in authorises nothing", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		appointment := s.appointCommittee(committee)
		appointment.ActivationTime, appointment.ExpiryTime = heightOrigin, heightOrigin+day
		s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, appointment))
		s.fundInsurance(5_000)
		s.setBlockTime(20)
		s.Require().ErrorContains(committeeSubmit(), "Claims mandate: mandate is not active")
		resp, err := s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().False(resp.Active)
	})
}

// TestEndBlockerBoundsSettlementsPerBlock proves a block settles at most MaxSettlementsPerBlock
// claims, oldest first, and leaves the rest pending and due for the next block.
func (s *KeeperTestSuite) TestEndBlockerBoundsSettlementsPerBlock() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockTime(20)
	s.fundInsurance(5_000)

	const total = types.MaxSettlementsPerBlock + 1
	claimIDs := make([]uint64, 0, total)
	for range total {
		claimIDs = append(claimIDs, s.submitGovernanceClaim(1))
	}

	s.setBlockTime(25)
	s.expectPayouts(types.MaxSettlementsPerBlock)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	settled, last := claimIDs[:types.MaxSettlementsPerBlock], claimIDs[types.MaxSettlementsPerBlock]
	for _, claimID := range settled {
		s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_PAID)
	}
	s.requireStatus(last, types.ClaimStatus_CLAIM_STATUS_PENDING)
	s.requireDueClaims(last)

	// Past its closing time the claim is beyond cancellation while it waits.
	_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Authority: s.authority,
		ClaimId:   last,
	})
	s.Require().ErrorContains(err, "cancellation period ended")

	s.setBlockTime(26)
	s.expectPayouts(1)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	s.requireStatus(last, types.ClaimStatus_CLAIM_STATUS_PAID)
	s.requireDueClaims()

	reserved, err := s.reserved(chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero())
}

// TestEndBlockerFailsRefusedPayment proves a payment Bank refuses after every pre-send check passed
// fails its claim rather than the block, and the sweep goes on to the next claim.
func (s *KeeperTestSuite) TestEndBlockerFailsRefusedPayment() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockTime(20)
	s.fundInsurance(5_000)
	refused := s.submitGovernanceClaim(100)
	paid := s.submitGovernanceClaim(200)

	// The recipient is not blocked and coverage holds, so only Bank's own restrictions can refuse.
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToAccount(gomock.Any(), types.InsuranceName, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ string, _ sdk.AccAddress, coins sdk.Coins) error {
			amount := coins.AmountOf(chain.NoahBaseDenom)
			if amount.Equal(math.NewInt(100)) {
				return errors.New("restricted recipient")
			}
			s.insurance = s.insurance.Sub(coins...)
			return nil
		}).
		Times(2)

	s.setBlockTime(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(refused, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireStatus(paid, types.ClaimStatus_CLAIM_STATUS_PAID)
	s.requireDueClaims()
	s.Require().Equal("4800", s.insurance.AmountOf(chain.NoahBaseDenom).String())

	reserved, err := s.reserved(chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a refused claim must release its reservation")
}

// TestEndBlockerFailsUnpayableClaim proves an unpayable claim ends failed
// rather than halting the block, staying pending, or silently vanishing — and
// that its reservation is released, since a claim that will never be paid must
// stop encumbering Insurance.
func (s *KeeperTestSuite) TestEndBlockerFailsUncoveredClaim() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockTime(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)
	other := s.submitGovernanceClaim(100)

	// Coverage is one of the two payability tests. It cannot go short while
	// Claims is the only Insurance outflow, so it is forced here. It fails both
	// claims: neither is payable at 50.
	s.setBlockTime(25)
	s.fundInsurance(50)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_FAILED)
	// One unpayable claim does not stop the sweep reaching the next.
	s.requireStatus(other, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	failed, err := s.keeper.Claims.Get(s.ctx, claimID)
	s.Require().NoError(err)
	s.Require().Equal(uint64(25), failed.ClosingTime)
	s.Require().Empty(failed.CancelledBy)

	reserved, err := s.reserved(chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")

	// Terminal on the first attempt: a later block with a funded account does
	// not resurrect it.
	s.setBlockTime(26)
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
	s.setBlockTime(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)

	// The recipient was payable at submission and is blocked before the claim
	// comes due. No expectPayouts: Bank must never be asked to pay it.
	s.blockedAddrs[testAddress(3)] = struct{}{}
	s.setBlockTime(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.reserved(chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")
}

// TestEndBlockerFailsInsuranceSelfPayment checks Claim.Validate rejects Insurance as its own
// recipient. Bank permits this address and denom, so only record validation prevents a false
// payment with unchanged custody.
func (s *KeeperTestSuite) TestEndBlockerFailsInsuranceSelfPayment() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockTime(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)

	// Submission refuses this recipient, so a stored claim can only carry it if
	// the rules moved under an immutable record. Written directly, the way such
	// a record would arrive. The closing time and ID are untouched, so the
	// queue entry stays valid and the claim still comes due.
	claim, err := s.keeper.Claims.Get(s.ctx, claimID)
	s.Require().NoError(err)
	claim.Recipient = authtypes.NewModuleAddress(types.InsuranceName).String()
	s.Require().NoError(s.keeper.Claims.Set(s.ctx, claimID, claim))

	// No expectPayouts: Bank must never be asked to pay it.
	s.setBlockTime(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(claimID, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.reserved(chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")
}

// TestEndBlockerDropsDivergentQueueEntry proves a queue entry that disagrees
// with its claim is discarded without touching the record. Marking it failed
// would overwrite the status of a claim that was already settled.
func (s *KeeperTestSuite) TestEndBlockerDropsDivergentQueueEntry() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockTime(20)
	s.fundInsurance(5_000)
	claimID := s.submitGovernanceClaim(100)

	s.setBlockTime(22)
	_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
		Authority: s.authority,
		ClaimId:   claimID,
	})
	s.Require().NoError(err)

	// Re-queue the cancelled claim by hand, which is the shape any index/record
	// divergence would take. No payout is arranged: a Bank send here fails the
	// mock.
	s.Require().NoError(s.keeper.DueClaims.Set(s.ctx, collections.Join(uint64(25), claimID)))

	s.setBlockTime(25)
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
	s.Require().NoError(s.keeper.DueClaims.Set(s.ctx, collections.Join(claim.ClosingTime, claim.ClaimId)))
	s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, chain.NoahBaseDenom, claim.Amount.Amount))
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
		ClosingTime:     25,
	})

	// No payout is arranged: Bank must never be asked to pay it.
	s.setBlockTime(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(1, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.reserved(chain.NoahBaseDenom)
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
		ClosingTime:     25,
	})

	// No payout is arranged: Bank must never be asked to pay it.
	s.setBlockTime(25)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))

	s.requireStatus(1, types.ClaimStatus_CLAIM_STATUS_FAILED)
	s.requireDueClaims()

	reserved, err := s.reserved(chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().True(reserved.IsZero(), "a failed claim must release its reservation")
}
