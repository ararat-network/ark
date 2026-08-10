package keeper_test

import (
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/claims/types"
)

// setCancellationPeriod pins a short veto window so test heights stay readable.
func (s *KeeperTestSuite) setCancellationPeriod(blocks uint64) {
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.Params{
		ClaimCancellationPeriodBlocks: blocks,
	}))
}

func (s *KeeperTestSuite) TestUpdateParams() {
	tests := []struct {
		name      string
		authority string
		params    types.Params
		expectErr string
	}{
		{
			name:      "governance authority",
			authority: s.authority,
			params:    types.Params{ClaimCancellationPeriodBlocks: 42},
		},
		{
			name:      "wrong authority",
			authority: testAddress(9),
			params:    types.Params{ClaimCancellationPeriodBlocks: 42},
			expectErr: "invalid authority",
		},
		{
			name:      "invalid params",
			authority: s.authority,
			params:    types.Params{ClaimCancellationPeriodBlocks: 0},
			expectErr: "ClaimCancellationPeriodBlocks must be between one and",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
				Authority: tc.authority,
				Params:    tc.params,
			})
			if tc.expectErr != "" {
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}
			s.Require().NoError(err)
			stored, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(tc.params, stored)
		})
	}
}

func (s *KeeperTestSuite) TestSetClaimsMandate() {
	committee := testAddress(1)

	s.Run("appoints and derives the next term", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		_, err := s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
			Authority:           s.authority,
			Committee:           committee,
			ActivationHeight:    10,
			ExpiryHeight:        100,
			CommitteeClaimLimit: noahCoin(1_000),
		})
		s.Require().NoError(err)

		stored, err := s.keeper.ClaimsMandate.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(uint64(1), stored.Term)
		s.Require().Equal(committee, stored.Committee)
		s.Require().Equal(noahCoin(1_000), stored.CommitteeClaimLimit)
	})

	s.Run("replacement resets allowance usage", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.NewInt(400)))

		_, err := s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
			Authority:           s.authority,
			Committee:           testAddress(2),
			ActivationHeight:    10,
			ExpiryHeight:        100,
			CommitteeClaimLimit: noahCoin(500),
		})
		s.Require().NoError(err)

		used, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(used.IsZero())
	})

	s.Run("elapsed window is rejected", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		// At the expiry height itself the window is already inactive, so this
		// pins the boundary along with the fully elapsed case.
		s.setBlockHeight(100)

		_, err := s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
			Authority:           s.authority,
			Committee:           committee,
			ActivationHeight:    10,
			ExpiryHeight:        100,
			CommitteeClaimLimit: noahCoin(1_000),
		})
		s.Require().ErrorContains(err, "is not after current height")
	})

	s.Run("partially elapsed window is accepted", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(50)

		_, err := s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
			Authority:           s.authority,
			Committee:           committee,
			ActivationHeight:    10,
			ExpiryHeight:        100,
			CommitteeClaimLimit: noahCoin(1_000),
		})
		s.Require().NoError(err)
	})

	s.Run("empty committee disables and keeps the reservation", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.NewInt(70)))

		_, err := s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
			Authority:           s.authority,
			CommitteeClaimLimit: noahCoin(0),
		})
		s.Require().NoError(err)

		stored, err := s.keeper.ClaimsMandate.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(stored.IsDisabled())
		reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(70), reserved)
	})

	tests := []struct {
		name      string
		msg       *types.MsgSetClaimsMandate
		expectErr string
	}{
		{
			name: "wrong authority",
			msg: &types.MsgSetClaimsMandate{
				Authority:           testAddress(9),
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        100,
				CommitteeClaimLimit: noahCoin(1_000),
			},
			expectErr: "invalid authority",
		},
		{
			name: "committee equals authority",
			msg: &types.MsgSetClaimsMandate{
				Authority:           s.authority,
				Committee:           s.authority,
				ActivationHeight:    10,
				ExpiryHeight:        100,
				CommitteeClaimLimit: noahCoin(1_000),
			},
			expectErr: "must be distinct from the Claims authority",
		},
		{
			name: "span shorter than cancellation period",
			msg: &types.MsgSetClaimsMandate{
				Authority:           s.authority,
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        13,
				CommitteeClaimLimit: noahCoin(1_000),
			},
			expectErr: "cannot be shorter than the claim cancellation period",
		},
		{
			name: "span equal to cancellation period is accepted",
			msg: &types.MsgSetClaimsMandate{
				Authority:           s.authority,
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        15,
				CommitteeClaimLimit: noahCoin(1_000),
			},
		},
		{
			name: "non-positive claim limit",
			msg: &types.MsgSetClaimsMandate{
				Authority:           s.authority,
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        100,
				CommitteeClaimLimit: noahCoin(0),
			},
			expectErr: "claim limit must be positive",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setCancellationPeriod(5)
			_, err := s.msgServer.SetClaimsMandate(s.ctx, tc.msg)
			if tc.expectErr == "" {
				s.Require().NoError(err)
			} else {
				s.Require().ErrorContains(err, tc.expectErr)
			}
		})
	}
}

func (s *KeeperTestSuite) TestSubmitClaim() {
	recipient := testAddress(3)

	s.Run("reserves without moving coins", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(500)

		resp, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
			Authority: s.authority,
			Reference: "incident",
			Recipient: recipient,
			Amount:    noahCoin(100),
		})
		s.Require().NoError(err)
		s.Require().Equal(uint64(1), resp.ClaimId)

		claim, err := s.keeper.Claims.Get(s.ctx, resp.ClaimId)
		s.Require().NoError(err)
		s.Require().Equal(types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE, claim.Origin)
		s.Require().Equal(uint64(0), claim.MandateTerm)
		s.Require().Equal(uint64(20), claim.SubmittedHeight)
		s.Require().Equal(uint64(25), claim.ClosingHeight)

		reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(100), reserved)

		// Governance submissions never touch the committee allowance.
		used, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(used.IsZero())
	})

	s.Run("uppercase recipient is stored canonical", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(500)

		resp, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
			Authority: s.authority,
			Reference: "incident",
			Recipient: strings.ToUpper(recipient),
			Amount:    noahCoin(100),
		})
		s.Require().NoError(err)

		claim, err := s.keeper.Claims.Get(s.ctx, resp.ClaimId)
		s.Require().NoError(err)
		s.Require().Equal(recipient, claim.Recipient)
	})

	s.Run("claim IDs are globally monotonic", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(500)

		for expected := uint64(1); expected <= 3; expected++ {
			resp, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
				Authority: s.authority,
				Reference: "incident",
				Recipient: recipient,
				Amount:    noahCoin(10),
			})
			s.Require().NoError(err)
			s.Require().Equal(expected, resp.ClaimId)
		}
	})

	tests := []struct {
		name      string
		balance   int64
		blocked   bool
		amount    sdk.Coin
		authority string
		recipient string
		expectErr string
	}{
		{
			name:      "wrong authority",
			balance:   500,
			amount:    noahCoin(100),
			authority: testAddress(9),
			recipient: recipient,
			expectErr: "invalid authority",
		},
		{
			name:      "uncovered by Insurance balance",
			balance:   50,
			amount:    noahCoin(100),
			authority: s.authority,
			recipient: recipient,
			expectErr: "cannot cover Insurance reservation",
		},
		{
			name:      "exact coverage is accepted",
			balance:   100,
			amount:    noahCoin(100),
			authority: s.authority,
			recipient: recipient,
		},
		{
			name:      "blocked recipient",
			balance:   500,
			blocked:   true,
			amount:    noahCoin(100),
			authority: s.authority,
			recipient: recipient,
			expectErr: "blocked from receiving funds",
		},
		{
			name:      "non-NOAH amount",
			balance:   500,
			amount:    sdk.NewInt64Coin("usdr", 100),
			authority: s.authority,
			recipient: recipient,
			expectErr: "claim amount must be denominated in anoah",
		},
		{
			name:      "zero amount",
			balance:   500,
			amount:    noahCoin(0),
			authority: s.authority,
			recipient: recipient,
			expectErr: "claim amount must be positive",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setCancellationPeriod(5)
			s.setBlockHeight(20)
			s.fundInsurance(tc.balance)
			if tc.blocked {
				s.blockedAddrs[tc.recipient] = struct{}{}
			}

			_, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
				Authority: tc.authority,
				Reference: "incident",
				Recipient: tc.recipient,
				Amount:    tc.amount,
			})
			if tc.expectErr == "" {
				s.Require().NoError(err)
			} else {
				s.Require().ErrorContains(err, tc.expectErr)
			}
		})
	}
}

func (s *KeeperTestSuite) TestCommitteeSubmitClaim() {
	committee := testAddress(1)
	recipient := testAddress(3)

	submit := func(amount int64) (*types.MsgCommitteeSubmitClaimResponse, error) {
		return s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
			Committee:    committee,
			ExpectedTerm: 1,
			Reference:    "incident",
			Recipient:    recipient,
			Amount:       noahCoin(amount),
		})
	}

	s.Run("consumes the term allowance", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)

		resp, err := submit(400)
		s.Require().NoError(err)

		claim, err := s.keeper.Claims.Get(s.ctx, resp.ClaimId)
		s.Require().NoError(err)
		s.Require().Equal(types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE, claim.Origin)
		s.Require().Equal(uint64(1), claim.MandateTerm)

		used, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(400), used)
	})

	s.Run("uppercase committee signer authorises and is stored canonical", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)

		resp, err := s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
			Committee:    strings.ToUpper(committee),
			ExpectedTerm: 1,
			Reference:    "incident",
			Recipient:    recipient,
			Amount:       noahCoin(100),
		})
		s.Require().NoError(err)

		claim, err := s.keeper.Claims.Get(s.ctx, resp.ClaimId)
		s.Require().NoError(err)
		s.Require().Equal(committee, claim.Submitter)
	})

	s.Run("allowance is consumed permanently across submissions", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)

		_, err := submit(600)
		s.Require().NoError(err)
		_, err = submit(500)
		s.Require().ErrorContains(err, "past mandate limit")

		// The exact remainder still fits.
		_, err = submit(400)
		s.Require().NoError(err)
	})

	s.Run("closing height may not pass mandate expiry", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.fundInsurance(5_000)

		// 96 + 5 = 101, one past expiry.
		s.setBlockHeight(96)
		_, err := submit(100)
		s.Require().ErrorContains(err, "exceeds Claims mandate expiry height")

		// 95 + 5 = 100, exactly at expiry, which is permitted.
		s.setBlockHeight(95)
		_, err = submit(100)
		s.Require().NoError(err)
	})

	tests := []struct {
		name      string
		committee string
		term      uint64
		height    int64
		expectErr string
	}{
		{name: "authorised", committee: committee, term: 1, height: 20},
		{
			name:      "wrong committee",
			committee: testAddress(8),
			term:      1,
			height:    20,
			expectErr: "Claims mandate",
		},
		{
			name:      "stale expected term",
			committee: committee,
			term:      0,
			height:    20,
			expectErr: "Claims mandate",
		},
		{
			name:      "before activation",
			committee: committee,
			term:      1,
			height:    9,
			expectErr: "Claims mandate",
		},
		{
			name:      "at expiry",
			committee: committee,
			term:      1,
			height:    100,
			expectErr: "Claims mandate",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setCancellationPeriod(5)
			s.appointCommittee(committee, 1, 10, 100, 1_000)
			s.setBlockHeight(tc.height)
			s.fundInsurance(5_000)

			_, err := s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
				Committee:    tc.committee,
				ExpectedTerm: tc.term,
				Reference:    "incident",
				Recipient:    recipient,
				Amount:       noahCoin(100),
			})
			if tc.expectErr == "" {
				s.Require().NoError(err)
			} else {
				s.Require().ErrorContains(err, tc.expectErr)
			}
		})
	}
}

func (s *KeeperTestSuite) TestCancelClaim() {
	committee := testAddress(1)
	recipient := testAddress(3)

	// submitGovernance returns a pending governance claim at height 20 with a
	// five-block veto window.
	submitGovernance := func() uint64 {
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)
		resp, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
			Authority: s.authority,
			Reference: "incident",
			Recipient: recipient,
			Amount:    noahCoin(100),
		})
		s.Require().NoError(err)
		return resp.ClaimId
	}

	s.Run("releases the reservation", func() {
		s.SetupTest()
		claimID := submitGovernance()
		s.setBlockHeight(24)

		_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   claimID,
		})
		s.Require().NoError(err)

		claim, err := s.keeper.Claims.Get(s.ctx, claimID)
		s.Require().NoError(err)
		s.Require().Equal(types.ClaimStatus_CLAIM_STATUS_CANCELLED, claim.Status)
		s.Require().Equal(types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE, claim.CancelledBy)

		reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(reserved.IsZero())

		// A cancelled claim must leave the settlement queue, or EndBlock would
		// pay it the moment its closing height arrived.
		s.requireDueClaims()
	})

	s.Run("cancels in the submission block", func() {
		s.SetupTest()
		claimID := submitGovernance()

		// Still at the submission height: a veto may land in the very block
		// that submitted the claim.
		_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   claimID,
		})
		s.Require().NoError(err)

		claim, err := s.keeper.Claims.Get(s.ctx, claimID)
		s.Require().NoError(err)
		s.Require().Equal(types.ClaimStatus_CLAIM_STATUS_CANCELLED, claim.Status)
		s.Require().Equal(claim.SubmittedHeight, claim.ClosingHeight)

		reserved, err := s.keeper.InsuranceReserved.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(reserved.IsZero())
		s.requireDueClaims()
	})

	s.Run("cancellation does not restore committee allowance", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)

		resp, err := s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
			Committee:    committee,
			ExpectedTerm: 1,
			Reference:    "incident",
			Recipient:    recipient,
			Amount:       noahCoin(400),
		})
		s.Require().NoError(err)

		s.setBlockHeight(24)
		_, err = s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   resp.ClaimId,
		})
		s.Require().NoError(err)

		used, err := s.keeper.ClaimsAllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(400), used)
	})

	s.Run("at closing height the window has closed", func() {
		s.SetupTest()
		claimID := submitGovernance()
		s.setBlockHeight(25)

		_, err := s.msgServer.CancelClaim(s.ctx, &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   claimID,
		})
		s.Require().ErrorContains(err, "cancellation period ended")
	})

	s.Run("committee cannot cancel a governance claim", func() {
		s.SetupTest()
		claimID := submitGovernance()
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.setBlockHeight(24)

		_, err := s.msgServer.CommitteeCancelClaim(s.ctx, &types.MsgCommitteeCancelClaim{
			Committee:    committee,
			ExpectedTerm: 1,
			ClaimId:      claimID,
		})
		s.Require().ErrorContains(err, "cannot cancel governance-submitted claim")
	})

	s.Run("committee cancels its own claim", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)

		resp, err := s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
			Committee:    committee,
			ExpectedTerm: 1,
			Reference:    "incident",
			Recipient:    recipient,
			Amount:       noahCoin(100),
		})
		s.Require().NoError(err)

		s.setBlockHeight(24)
		_, err = s.msgServer.CommitteeCancelClaim(s.ctx, &types.MsgCommitteeCancelClaim{
			Committee:    committee,
			ExpectedTerm: 1,
			ClaimId:      resp.ClaimId,
		})
		s.Require().NoError(err)

		claim, err := s.keeper.Claims.Get(s.ctx, resp.ClaimId)
		s.Require().NoError(err)
		s.Require().Equal(types.ClaimStatus_CLAIM_STATUS_CANCELLED, claim.Status)
		s.Require().Equal(types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE, claim.CancelledBy)
	})

	s.Run("already cancelled claim is not pending", func() {
		s.SetupTest()
		claimID := submitGovernance()
		s.setBlockHeight(22)

		cancel := &types.MsgCancelClaim{
			Authority: s.authority,
			ClaimId:   claimID,
		}
		_, err := s.msgServer.CancelClaim(s.ctx, cancel)
		s.Require().NoError(err)
		_, err = s.msgServer.CancelClaim(s.ctx, cancel)
		s.Require().ErrorContains(err, "is not pending")
	})
}
