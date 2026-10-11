package simulation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/disbursement/simulation"
)

// TestSignedFactoriesReachEveryCommitteePower is the reachability proof. Suspension needs a
// registered member and reinstatement a suspended one, so the factories run in that order: one
// tested alone would skip for a reason no run could tell from a broken one.
func TestSignedFactoriesReachEveryCommitteePower(t *testing.T) {
	f := newDisbursementFixture(t)
	committee, registrar := f.appoint(t)
	f.openTranche(t)

	signers, register := simulation.MsgRegisterMembersFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Len(t, signers, 1)
	require.Equal(t, registrar.Committee, signers[0].Address.String(), "the appointed registrar must sign")
	require.Equal(t, registrar.Term, register.ExpectedTerm)
	registered, err := f.msgServer.RegisterMembers(f.ctx, register)
	require.NoError(t, err)
	require.Len(t, registered.GrantIds, len(register.Addresses))

	signers, suspend := simulation.MsgCommitteeSuspendFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, committee.Committee, signers[0].Address.String(), "the appointed committee must sign")
	require.Subset(t, register.Addresses, suspend.Addresses)
	_, err = f.msgServer.CommitteeSuspend(f.ctx, suspend)
	require.NoError(t, err)

	_, reinstate := simulation.MsgCommitteeReinstateFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Subset(t, suspend.Addresses, reinstate.Addresses)
	_, err = f.msgServer.CommitteeReinstate(f.ctx, reinstate)
	require.NoError(t, err)

	_, compensate := simulation.MsgCommitteeCompensateFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, committee.Term, compensate.ExpectedTerm)
	// The trial run wrote nothing; the delivery below is the award's only write.
	used, err := f.keeper.CompensationUsed.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, used.IsZero())
	_, err = f.msgServer.CommitteeCompensate(f.ctx, compensate)
	require.NoError(t, err)

	_, appoint := simulation.MsgCommitteeRegistrarFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, committee.Term, appoint.ExpectedTerm)
	_, err = f.msgServer.CommitteeRegistrar(f.ctx, appoint)
	require.NoError(t, err)
	replaced, err := f.keeper.RegistrarMandate.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, registrar.Term+1, replaced.Term)
}

func TestSignedFactoriesSkipWithoutAnActiveAppointment(t *testing.T) {
	tests := map[string]struct {
		emit    func(f disbursementFixture) any
		comment string
	}{
		"register members": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgRegisterMembersFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "registrar mandate is not active",
		},
		"committee registrar": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgCommitteeRegistrarFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "grants mandate is not active",
		},
		"committee suspend": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgCommitteeSuspendFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "grants mandate is not active",
		},
		"committee reinstate": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgCommitteeReinstateFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "grants mandate is not active",
		},
		"committee compensate": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgCommitteeCompensateFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "grants mandate is not active",
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

// A run's opening blocks have appointments but, until a tranche opens, nothing to register against.
func TestMsgRegisterMembersFactorySkipsWithoutATranche(t *testing.T) {
	f := newDisbursementFixture(t)
	f.appoint(t)

	_, msg := simulation.MsgRegisterMembersFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "pays for no member")
	require.Nil(t, msg)
}

// The trial run is what keeps a refused award out of delivery; a zero allowance refuses every award,
// and the refusal writes nothing.
func TestMsgCommitteeCompensateFactorySkipsWhatTheHandlerRefuses(t *testing.T) {
	f := newDisbursementFixture(t)
	committee, _ := f.appoint(t)
	committee.CompensationAllowance = math.ZeroInt()
	require.NoError(t, f.keeper.GrantsMandate.Set(f.ctx, committee))

	_, msg := simulation.MsgCommitteeCompensateFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "compensation would be refused")
	require.Nil(t, msg)
	has, err := f.keeper.Grants.Has(f.ctx, 1)
	require.NoError(t, err)
	require.False(t, has)
	used, err := f.keeper.CompensationUsed.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, used.IsZero())
	next, err := f.keeper.NextGrantID.Peek(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), next)
}
