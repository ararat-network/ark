package keeper_test

import (
	"github.com/cosmos/gogoproto/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/x/security/types"
)

func (s *KeeperTestSuite) TestSetSecurityMandate() {
	stranger := authtypes.NewModuleAddress("stranger").String()

	s.Run("appoints and derives the term", func() {
		_, err := s.msgServer.SetSecurityMandate(s.ctx, &types.MsgSetSecurityMandate{
			Authority:      s.authority,
			Committee:      s.committee,
			ActivationTime: 50,
			ExpiryTime:     500,
		})
		s.Require().NoError(err)

		appointment, getErr := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().Equal(uint64(1), appointment.Term)
		s.Require().Equal(s.committee, appointment.Committee)
	})

	s.Run("replacement advances the term", func() {
		_, err := s.msgServer.SetSecurityMandate(s.ctx, &types.MsgSetSecurityMandate{
			Authority:      s.authority,
			Committee:      stranger,
			ActivationTime: 50,
			ExpiryTime:     500,
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
				Authority:      authtypes.NewModuleAddress("stranger").String(),
				Committee:      s.committee,
				ActivationTime: 50,
				ExpiryTime:     500,
			},
			expected: "invalid authority",
		},
		{
			name: "committee is the authority",
			msg: &types.MsgSetSecurityMandate{
				Authority:      s.authority,
				Committee:      s.authority,
				ActivationTime: 50,
				ExpiryTime:     500,
			},
			expected: "security committee must be distinct from the chain authority",
		},
		{
			name: "expiry already behind the chain",
			msg: &types.MsgSetSecurityMandate{
				Authority:      s.authority,
				Committee:      s.committee,
				ActivationTime: 10,
				ExpiryTime:     100,
			},
			expected: "which is not after the current block time 100",
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
		s.Require().Equal(uint64(200), record.Height)
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
		s.Require().Equal(uint64(210), record.Height)
	})

	s.Run("refuses an exact repeat of its own plan, accepts new info", func() {
		s.router.dispatched = nil
		s.upgrade.plan = &upgradetypes.Plan{Name: committeeUpgradeNameB, Height: 210}

		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeNameB,
			Height:       210,
		})
		s.Require().ErrorContains(err, "is already pending at height 210")
		s.Require().Empty(s.router.dispatched)

		_, err = s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeNameB,
			Height:       210,
			Info:         "corrected binaries",
		})
		s.Require().NoError(err)
		dispatched, ok := s.router.only().(*upgradetypes.MsgSoftwareUpgrade)
		s.Require().True(ok)
		s.Require().Equal("corrected binaries", dispatched.Plan.Info)
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

	s.Run("refuses a height x/upgrade cannot store", func() {
		s.upgrade.plan = nil
		_, err := s.msgServer.CommitteePlanUpgrade(s.ctx, &types.MsgCommitteePlanUpgrade{
			Committee:    s.committee,
			ExpectedTerm: 1,
			Name:         committeeUpgradeName,
			Height:       types.MaxPlanHeight + 1,
		})
		s.Require().ErrorContains(err, "exceeds x/upgrade's maximum")
		s.Require().Empty(s.router.dispatched)
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

// TestDispatchForwardsUpstreamEvents pins that what a target emits reaches the
// transaction result. The router gives the target its own event manager, so a
// dispatch that drops the result would leave only this module's own event.
func (s *KeeperTestSuite) TestDispatchForwardsUpstreamEvents() {
	s.appoint()
	s.router.events = sdk.Events{
		sdk.NewEvent("software_upgrade", sdk.NewAttribute("name", committeeUpgradeName)),
	}
	ctx := s.ctx.WithEventManager(sdk.NewEventManager())

	_, err := s.msgServer.CommitteePlanUpgrade(ctx, &types.MsgCommitteePlanUpgrade{
		Committee:    s.committee,
		ExpectedTerm: 1,
		Name:         committeeUpgradeName,
		Height:       200,
	})
	s.Require().NoError(err)

	emitted := ctx.EventManager().Events()
	s.Require().Len(emitted, 2)
	s.Require().Equal("software_upgrade", emitted[0].Type)
	s.Require().Equal(proto.MessageName(&types.EventCommitteeUpgradeScheduled{}), emitted[1].Type)
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
}
