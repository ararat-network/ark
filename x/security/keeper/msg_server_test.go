package keeper_test

import (
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"ark/x/security/types"
)

func (s *KeeperTestSuite) TestSetSecurityMandate() {
	stranger := authtypes.NewModuleAddress("stranger").String()

	s.Run("appoints and derives the term", func() {
		_, err := s.msgServer.SetSecurityMandate(s.ctx, &types.MsgSetSecurityMandate{
			Authority:        s.authority,
			Committee:        s.committee,
			ActivationHeight: 50,
			ExpiryHeight:     500,
		})
		s.Require().NoError(err)

		appointment, getErr := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().Equal(uint64(1), appointment.Term)
		s.Require().Equal(s.committee, appointment.Committee)
	})

	s.Run("replacement advances the term", func() {
		_, err := s.msgServer.SetSecurityMandate(s.ctx, &types.MsgSetSecurityMandate{
			Authority:        s.authority,
			Committee:        stranger,
			ActivationHeight: 50,
			ExpiryHeight:     500,
		})
		s.Require().NoError(err)

		appointment, getErr := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().Equal(uint64(2), appointment.Term)
	})

	s.Run("disabling retains the term", func() {
		_, err := s.msgServer.SetSecurityMandate(s.ctx, &types.MsgSetSecurityMandate{
			Authority: s.authority,
		})
		s.Require().NoError(err)

		appointment, getErr := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().True(appointment.IsDisabled())
		s.Require().Equal(uint64(3), appointment.Term)
	})
}

func (s *KeeperTestSuite) TestSetSecurityMandateRejections() {
	testCases := []struct {
		name     string
		msg      *types.MsgSetSecurityMandate
		expected string
	}{
		{
			name: "wrong authority",
			msg: &types.MsgSetSecurityMandate{
				Authority:        authtypes.NewModuleAddress("stranger").String(),
				Committee:        s.committee,
				ActivationHeight: 50,
				ExpiryHeight:     500,
			},
			expected: "invalid authority",
		},
		{
			name: "committee is the authority",
			msg: &types.MsgSetSecurityMandate{
				Authority:        s.authority,
				Committee:        s.authority,
				ActivationHeight: 50,
				ExpiryHeight:     500,
			},
			expected: "security committee must be distinct from the chain authority",
		},
		{
			name: "expiry already behind the chain",
			msg: &types.MsgSetSecurityMandate{
				Authority:        s.authority,
				Committee:        s.committee,
				ActivationHeight: 10,
				ExpiryHeight:     100,
			},
			expected: "which is not above the current height 100",
		},
	}

	for _, testCase := range testCases {
		s.Run(testCase.name, func() {
			_, err := s.msgServer.SetSecurityMandate(s.ctx, testCase.msg)
			s.Require().ErrorContains(err, testCase.expected)
		})
	}
}

// TestCommitteeAuthorisationIsSharedAcrossHandlers exercises the envelope
// guard every committee message runs before its own preconditions, once per
// handler, so no handler can quietly skip it.
func (s *KeeperTestSuite) TestCommitteeAuthorisationIsSharedAcrossHandlers() {
	s.appoint()
	stranger := authtypes.NewModuleAddress("stranger").String()

	calls := []struct {
		name string
		call func(committee string, term uint64) error
	}{
		{
			name: "plan upgrade",
			call: func(committee string, term uint64) error {
				_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
					Committee: committee, ExpectedTerm: term, Name: "v2", Height: 200,
				})

				return err
			},
		},
		{
			name: "cancel upgrade",
			call: func(committee string, term uint64) error {
				_, err := s.msgServer.CommitteeCancelUpgrade(s.ctx, &types.MsgCommitteeCancelUpgrade{
					Committee: committee, ExpectedTerm: term,
				})

				return err
			},
		},
		{
			name: "recover client",
			call: func(committee string, term uint64) error {
				_, err := s.msgServer.CommitteeRecoverClient(s.ctx, &types.MsgCommitteeRecoverClient{
					Committee: committee, ExpectedTerm: term,
					SubjectClientId: testSubjectClient, SubstituteClientId: testSubstituteClient,
				})

				return err
			},
		},
	}

	for _, entry := range calls {
		s.Run(entry.name+" refuses a stranger", func() {
			s.Require().ErrorContains(entry.call(stranger, 1), notAppointedCommitteeErr)
		})
		s.Run(entry.name+" refuses a stale term", func() {
			s.Require().ErrorContains(entry.call(s.committee, 99), "security mandate: term mismatch")
		})
		s.Run(entry.name+" refuses the authority as committee", func() {
			s.Require().ErrorContains(entry.call(s.authority, 1), notAppointedCommitteeErr)
		})
	}

	s.Require().Empty(s.router.dispatched, "no unauthorised message reached the router")
}

func (s *KeeperTestSuite) TestCommitteePlanUpgrade() {
	s.appoint()

	s.Run("schedules into an empty slot", func() {
		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeName,
			Height:       200,
			Info:         "binaries",
		})
		s.Require().NoError(err)

		dispatched, ok := s.router.only().(*upgradetypes.MsgSoftwareUpgrade)
		s.Require().True(ok)
		s.Require().Equal(committeeUpgradeName, dispatched.Plan.Name)
		s.Require().Equal(int64(200), dispatched.Plan.Height)
		s.Require().Equal("binaries", dispatched.Plan.Info)

		record, getErr := s.keeper.CommitteePlan.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().Equal(committeeUpgradeName, record.Name)
		s.Require().Equal(int64(200), record.Height)
		s.Require().Equal(uint64(1), record.Term)
	})

	s.Run("replaces its own pending plan", func() {
		s.router.dispatched = nil
		s.upgrade.plan = &upgradetypes.Plan{Name: committeeUpgradeName, Height: 200}

		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeNameB,
			Height:       210,
		})
		s.Require().NoError(err)

		record, getErr := s.keeper.CommitteePlan.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().Equal(committeeUpgradeNameB, record.Name)
		s.Require().Equal(int64(210), record.Height)
	})

	s.Run("refuses to displace a governance plan", func() {
		s.router.dispatched = nil
		s.upgrade.plan = &upgradetypes.Plan{Name: governanceUpgradeName, Height: 900}

		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         "v2-emergency-c",
			Height:       220,
		})
		s.Require().ErrorContains(err, "was not scheduled by the committee: only governance replaces it")
		s.Require().Empty(s.router.dispatched)
	})

	// A record that no longer matches what is pending must read as "not ours",
	// or the committee would silently overwrite a plan the chain voted for.
	s.Run("a stale record does not claim the pending plan", func() {
		s.router.dispatched = nil
		s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, types.CommitteePlan{
			Name: committeeUpgradeNameB, Height: 210, Term: 1,
		}))
		s.upgrade.plan = &upgradetypes.Plan{Name: committeeUpgradeNameB, Height: 777}

		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         "v2-emergency-d",
			Height:       230,
		})
		s.Require().ErrorContains(err, "only governance replaces it")
		s.Require().Empty(s.router.dispatched)
	})

	s.Run("refuses an unnamed plan", func() {
		s.upgrade.plan = nil
		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Height:       240,
		})
		s.Require().ErrorContains(err, "upgrade plan must have a name")
	})
}

func (s *KeeperTestSuite) TestCommitteeCancelUpgrade() {
	s.appoint()

	s.Run("refuses when no plan is pending", func() {
		_, err := s.msgServer.CommitteeCancelUpgrade(s.ctx, &types.MsgCommitteeCancelUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
		})
		s.Require().ErrorContains(err, "no upgrade plan is pending")
		s.Require().Empty(s.router.dispatched)
	})

	// Cancelling a governance plan would empty the slot, after which the
	// schedule guard sees no pending plan and lets the committee schedule over
	// a decision the chain voted on.
	s.Run("refuses a governance plan", func() {
		s.router.dispatched = nil
		s.upgrade.plan = &upgradetypes.Plan{Name: governanceUpgradeName, Height: 900}

		_, err := s.msgServer.CommitteeCancelUpgrade(s.ctx, &types.MsgCommitteeCancelUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
		})
		s.Require().ErrorContains(err, "only governance cancels it")
		s.Require().Empty(s.router.dispatched)
	})

	s.Run("refuses a governance plan under a stale record", func() {
		s.router.dispatched = nil
		s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, types.CommitteePlan{
			Name: committeeUpgradeName, Height: 200, Term: 1,
		}))
		s.upgrade.plan = &upgradetypes.Plan{Name: committeeUpgradeName, Height: 777}

		_, err := s.msgServer.CommitteeCancelUpgrade(s.ctx, &types.MsgCommitteeCancelUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
		})
		s.Require().ErrorContains(err, "only governance cancels it")
		s.Require().Empty(s.router.dispatched)
	})

	s.Run("cancels its own plan and clears the record", func() {
		s.router.dispatched = nil
		s.upgrade.plan = &upgradetypes.Plan{Name: committeeUpgradeName, Height: 200}
		s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, types.CommitteePlan{
			Name: committeeUpgradeName, Height: 200, Term: 1,
		}))

		_, err := s.msgServer.CommitteeCancelUpgrade(s.ctx, &types.MsgCommitteeCancelUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
		})
		s.Require().NoError(err)

		record, getErr := s.keeper.CommitteePlan.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().True(record.IsZero())
	})
}

// TestCommitteeCannotReplaceAGovernancePlanInTwoSteps pins the hole that
// cancel-any left open: with cancellation restricted to the committee's own
// plan, a governance plan can be neither cleared nor scheduled over.
func (s *KeeperTestSuite) TestCommitteeCannotReplaceAGovernancePlanInTwoSteps() {
	s.appoint()
	s.upgrade.plan = &upgradetypes.Plan{Name: governanceUpgradeName, Height: 900}

	_, err := s.msgServer.CommitteeCancelUpgrade(s.ctx, &types.MsgCommitteeCancelUpgrade{
		Committee:    s.committee,
		ExpectedTerm: 1,
	})
	s.Require().ErrorContains(err, "only governance cancels it")

	_, err = s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
		Committee:    s.committee,
		ExpectedTerm: 1,
		Name:         committeeUpgradeName,
		Height:       200,
	})
	s.Require().ErrorContains(err, "only governance replaces it")
	s.Require().Empty(s.router.dispatched)
}

func (s *KeeperTestSuite) TestCommitteeRecoverClient() {
	s.appoint()

	s.Run("recovers", func() {
		_, err := s.msgServer.CommitteeRecoverClient(s.ctx, &types.MsgCommitteeRecoverClient{
			Committee:          s.committee,
			ExpectedTerm:       1,
			SubjectClientId:    testSubjectClient,
			SubstituteClientId: testSubstituteClient,
		})
		s.Require().NoError(err)

		dispatched, ok := s.router.only().(*clienttypes.MsgRecoverClient)
		s.Require().True(ok)
		s.Require().Equal(testSubjectClient, dispatched.SubjectClientId)
		s.Require().Equal(testSubstituteClient, dispatched.SubstituteClientId)
		s.Require().Equal(s.authority, dispatched.Signer)
	})

	s.Run("refuses identical clients", func() {
		_, err := s.msgServer.CommitteeRecoverClient(s.ctx, &types.MsgCommitteeRecoverClient{
			Committee:          s.committee,
			ExpectedTerm:       1,
			SubjectClientId:    testSubjectClient,
			SubstituteClientId: testSubjectClient,
		})
		s.Require().ErrorContains(err, "subject and substitute clients must differ")
	})

	s.Run("refuses a missing client", func() {
		_, err := s.msgServer.CommitteeRecoverClient(s.ctx, &types.MsgCommitteeRecoverClient{
			Committee:       s.committee,
			ExpectedTerm:    1,
			SubjectClientId: testSubjectClient,
		})
		s.Require().ErrorContains(err, "subject and substitute client identifiers must be set")
	})
}

// TestCommitteeMessagesRejectNil covers the defensive nil guard every handler
// opens with, which a direct caller can reach even though the router cannot.
func (s *KeeperTestSuite) TestCommitteeMessagesRejectNil() {
	_, err := s.msgServer.SetSecurityMandate(s.ctx, nil)
	s.Require().Error(err)
	_, err = s.msgServer.CommitteePlanUpgrade(s.ctx, nil)
	s.Require().Error(err)
	_, err = s.msgServer.CommitteeCancelUpgrade(s.ctx, nil)
	s.Require().Error(err)
	_, err = s.msgServer.CommitteeRecoverClient(s.ctx, nil)
	s.Require().Error(err)
}
