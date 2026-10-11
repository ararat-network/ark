package keeper_test

import (
	"errors"

	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/x/security/types"
)

// TestBeginBlockerReconcilesThePlanRecord pins that a record outliving its plan is dropped at the
// next block, so a governance plan that later reuses the name and height cannot read as the
// committee's, while a record the slot still holds is kept.
func (s *KeeperTestSuite) TestBeginBlockerReconcilesThePlanRecord() {
	record := types.CommitteePlan{Name: committeeUpgradeName, Height: 210, Term: 1}
	tests := []struct {
		name    string
		pending *upgradetypes.Plan
		kept    bool
	}{
		{name: "slot holds the committee plan", pending: &upgradetypes.Plan{Name: committeeUpgradeName, Height: 210}, kept: true},
		{name: "slot is empty", pending: nil},
		{name: "slot holds a governance plan", pending: &upgradetypes.Plan{Name: governanceUpgradeName, Height: 300}},
		{name: "slot holds the same name at another height", pending: &upgradetypes.Plan{Name: committeeUpgradeName, Height: 211}},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, record))
			s.upgrade.plan = tc.pending

			s.Require().NoError(s.keeper.BeginBlocker(s.ctx))

			stored, err := s.keeper.CommitteePlan.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(tc.kept, !stored.IsZero())
		})
	}

	// A store fault is not an empty slot, so the record is kept and the fault surfaces.
	s.Run("an unreadable slot keeps the record", func() {
		s.SetupTest()
		s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, record))
		fault := errors.New("upgrade store corrupted")
		s.upgrade.err = fault

		err := s.keeper.BeginBlocker(s.ctx)

		s.Require().ErrorIs(err, fault)
		s.Require().ErrorContains(err, "getting upgrade plan")
		stored, getErr := s.keeper.CommitteePlan.Get(s.ctx)
		s.Require().NoError(getErr)
		s.Require().Equal(record, stored)
	})

	s.Run("an empty record reads nothing else", func() {
		s.SetupTest()
		s.upgrade.plan = &upgradetypes.Plan{Name: governanceUpgradeName, Height: 300}
		s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
		stored, err := s.keeper.CommitteePlan.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(stored.IsZero())
	})
}
