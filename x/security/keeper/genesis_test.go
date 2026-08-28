package keeper_test

import (
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/x/security/types"
)

func (s *KeeperTestSuite) TestInitGenesis() {
	s.Run("default genesis", func() {
		s.Require().NoError(s.keeper.InitGenesis(s.ctx, types.DefaultGenesisState()))

		appointment, err := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(appointment.IsDisabled())
	})

	s.Run("enabled mandate with a recorded plan", func() {
		genesisState := types.DefaultGenesisState()
		genesisState.SecurityMandate = s.genesisAppointment()
		genesisState.CommitteePlan = types.CommitteePlan{Name: committeeUpgradeName, Height: 200, Term: 1}

		s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesisState))

		record, err := s.keeper.CommitteePlan.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(committeeUpgradeName, record.Name)
	})

	s.Run("nil genesis", func() {
		s.Require().ErrorContains(s.keeper.InitGenesis(s.ctx, nil), "genesis state is nil")
	})

	s.Run("invalid mandate", func() {
		genesisState := types.DefaultGenesisState()
		appointment := s.genesisAppointment()
		appointment.ExpiryHeight = appointment.ActivationHeight
		genesisState.SecurityMandate = appointment

		s.Require().ErrorContains(
			s.keeper.InitGenesis(s.ctx, genesisState),
			"activation height must precede expiry height",
		)
	})

	s.Run("committee is the authority", func() {
		genesisState := types.DefaultGenesisState()
		appointment := s.genesisAppointment()
		appointment.Committee = s.authority
		genesisState.SecurityMandate = appointment

		s.Require().ErrorContains(
			s.keeper.InitGenesis(s.ctx, genesisState),
			"security committee must be distinct from the chain authority",
		)
	})
}

// TestInitGenesisAcceptsAnExpiredMandate pins that an import carries whatever
// the export held. Appointment refuses an expiry behind the chain, but a chain
// must be able to start from an export taken after a mandate lapsed.
func (s *KeeperTestSuite) TestInitGenesisAcceptsAnExpiredMandate() {
	genesisState := types.DefaultGenesisState()
	appointment := s.genesisAppointment()
	appointment.ActivationHeight = 1
	appointment.ExpiryHeight = 2
	genesisState.SecurityMandate = appointment

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesisState))
}

func (s *KeeperTestSuite) TestExportGenesisRoundTrips() {
	original := types.DefaultGenesisState()
	original.SecurityMandate = s.genesisAppointment()
	original.CommitteePlan = types.CommitteePlan{Name: committeeUpgradeName, Height: 200, Term: 1}
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, original))

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(original, exported)
	s.Require().NoError(exported.Validate())
}

func (s *KeeperTestSuite) TestExportGenesisWithoutAPlan() {
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, types.DefaultGenesisState()))

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().True(exported.CommitteePlan.IsZero())
	s.Require().NoError(exported.Validate())
}

func (s *KeeperTestSuite) genesisAppointment() types.SecurityMandate {
	appointment := types.NewDisabledSecurityMandate(1)
	appointment.Committee = s.committee
	appointment.ActivationHeight = 50
	appointment.ExpiryHeight = 500

	return appointment
}

func (s *KeeperTestSuite) TestQuerySecurityMandate() {
	s.appoint()

	response, err := s.queryClient.SecurityMandate(s.ctx, &types.QuerySecurityMandateRequest{})
	s.Require().NoError(err)
	s.Require().Equal(s.committee, response.Mandate.Committee)
	s.Require().True(response.Active)

	// The query helper captured the suite context at height 100, so an
	// appointment opening later reads as inactive through the same client.
	future := types.NewDisabledSecurityMandate(2)
	future.Committee = s.committee
	future.ActivationHeight = 900
	future.ExpiryHeight = 1000
	s.Require().NoError(s.keeper.Mandate.Set(s.ctx, future))

	response, err = s.queryClient.SecurityMandate(s.ctx, &types.QuerySecurityMandateRequest{})
	s.Require().NoError(err)
	s.Require().False(response.Active)
}

// TestQueryCommitteePlan pins the field an operator reads: whether the
// recorded plan is still the pending one. A record alone says nothing, since
// governance may have replaced or cancelled it since.
func (s *KeeperTestSuite) TestQueryCommitteePlan() {
	s.Run("no record", func() {
		response, err := s.queryClient.CommitteePlan(s.ctx, &types.QueryCommitteePlanRequest{})
		s.Require().NoError(err)
		s.Require().True(response.CommitteePlan.IsZero())
		s.Require().False(response.MatchesPending)
	})

	s.Run("record matches the pending plan", func() {
		s.Require().NoError(s.keeper.CommitteePlan.Set(s.ctx, types.CommitteePlan{
			Name: committeeUpgradeName, Height: 200, Term: 1,
		}))
		s.upgrade.plan = &upgradetypes.Plan{Name: committeeUpgradeName, Height: 200}

		response, err := s.queryClient.CommitteePlan(s.ctx, &types.QueryCommitteePlanRequest{})
		s.Require().NoError(err)
		s.Require().True(response.MatchesPending)
	})

	s.Run("governance replaced the plan", func() {
		s.upgrade.plan = &upgradetypes.Plan{Name: governanceUpgradeName, Height: 900}

		response, err := s.queryClient.CommitteePlan(s.ctx, &types.QueryCommitteePlanRequest{})
		s.Require().NoError(err)
		s.Require().Equal(committeeUpgradeName, response.CommitteePlan.Name)
		s.Require().False(response.MatchesPending)
	})

	s.Run("the plan executed or was cancelled", func() {
		s.upgrade.plan = nil

		response, err := s.queryClient.CommitteePlan(s.ctx, &types.QueryCommitteePlanRequest{})
		s.Require().NoError(err)
		s.Require().False(response.MatchesPending)
	})
}
