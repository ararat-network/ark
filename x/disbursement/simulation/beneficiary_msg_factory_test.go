package simulation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"github.com/ararat-network/ark/x/disbursement/simulation"
)

// TestBeneficiaryFactoriesRedirectAndRotate covers both kinds of controller: a member signs for its
// own grant, a contributor record's controller for the record's grants.
func TestBeneficiaryFactoriesRedirectAndRotate(t *testing.T) {
	f := newDisbursementFixture(t)
	f.appoint(t)
	f.openTranche(t)
	f.register(t)
	_, award := simulation.MsgCommitteeCompensateFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err := f.msgServer.CommitteeCompensate(f.ctx, award)
	require.NoError(t, err)

	// The draw may land on the current payee, which skips; enough attempts reach a change.
	redirected := false
	for range 10 {
		f.reporter = simsx.NewBasicSimulationReporter()
		signers, msg := simulation.MsgSetPayeeFactory(f.keeper)(f.ctx, f.testData, f.reporter)
		if f.reporter.IsSkipped() {
			require.Contains(t, f.reporter.Comment(), "drawn payee is the current one")
			continue
		}
		require.Equal(t, msg.Sender, signers[0].Address.String(), "the controller must sign")
		_, err := f.msgServer.SetPayee(f.ctx, msg)
		require.NoError(t, err)
		g, err := f.keeper.Grants.Get(f.ctx, msg.GrantId)
		require.NoError(t, err)
		require.Equal(t, msg.Payee, g.Payee)
		redirected = true
	}
	require.True(t, redirected)

	rotated := false
	for range 10 {
		f.reporter = simsx.NewBasicSimulationReporter()
		signers, msg := simulation.MsgSetControllerFactory(f.keeper)(f.ctx, f.testData, f.reporter)
		if f.reporter.IsSkipped() {
			require.Contains(t, f.reporter.Comment(), "drawn controller is the current one")
			continue
		}
		require.Equal(t, msg.Sender, signers[0].Address.String(), "the current controller must sign")
		_, err := f.msgServer.SetController(f.ctx, msg)
		require.NoError(t, err)
		rotated = true
	}
	require.True(t, rotated)
}

func TestBeneficiaryFactoriesSkipWithoutAGrant(t *testing.T) {
	tests := map[string]struct {
		emit    func(f disbursementFixture) any
		comment string
	}{
		"set payee": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgSetPayeeFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "no grant with remaining entitlement",
		},
		"set controller": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgSetControllerFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "no contributor record",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newDisbursementFixture(t)

			msg := tc.emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), tc.comment)
			require.Nil(t, msg)
		})
	}
}
