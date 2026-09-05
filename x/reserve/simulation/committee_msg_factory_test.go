package simulation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/reserve/simulation"
	"github.com/ararat-network/ark/x/reserve/types"
)

// The committee surface is signed rather than proposed, so a run reaches it
// only through these factories. Each reads live state and skips when its
// precondition does not hold, and a simulation cannot tell a factory that
// never applies from one that never could: both report as skips. What follows
// therefore asserts reachability first — that from the state a run actually
// starts in, every factory does produce a message its own handler accepts —
// and the skip paths second.

// requireActiveMandate returns the appointment the fixture installed, which
// every committee message has to name.
func requireActiveMandate(t *testing.T, f reserveFixture) types.ReserveMandate {
	t.Helper()

	mandate, err := f.keeper.Mandate.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, mandate.IsActive(uint64(f.ctx.BlockHeight())))

	return mandate
}

// TestCommitteeFactoriesDriveThePositionLifecycle is the reachability proof.
// The factories are chained in the order a run would reach them — a deployment
// opens the position the rest act on — and every message is delivered through
// the handler it targets. A factory that has drifted out of its handler's
// domain, or whose precondition no live state can satisfy, fails here rather
// than quietly reporting a skip for the life of the nightly run.
func TestCommitteeFactoriesDriveThePositionLifecycle(t *testing.T) {
	f := newReserveFixture(t, 1_000_000, withActiveCommittee(), withTreasuryRequirement(math.ZeroInt()))
	mandate := requireActiveMandate(t, f)

	signers, deploy := simulation.MsgCommitteeDeployFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.NotNil(t, deploy)
	require.Len(t, signers, 1)
	require.Equal(t, mandate.Committee, signers[0].Address.String(), "the appointed committee must sign")
	require.Equal(t, mandate.Term, deploy.ExpectedTerm)
	require.True(t, mandate.AllowsDestination(deploy.Destination))
	deployed, err := f.msgServer.CommitteeDeploy(f.ctx, deploy)
	require.NoError(t, err)
	require.NotZero(t, deployed.PositionId)

	t.Run("record update restates the open position", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeRecordUpdateFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, deployed.PositionId, msg.PositionId)
		require.Equal(t, deploy.Acquired.Denom, msg.Quantity.Denom, "a restatement cannot change the denomination")
		_, err := f.msgServer.CommitteeRecordUpdate(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("attribute return books value coming back", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeAttributeReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, deployed.PositionId, msg.PositionId)
		_, err := f.msgServer.CommitteeAttributeReturn(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("reverse return withdraws that attribution", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeReverseReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, deployed.PositionId, msg.PositionId)
		require.NotZero(t, msg.Reverses)
		_, err := f.msgServer.CommitteeReverseReturn(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("correct position restates the deployment entry", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeCorrectPositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, deployed.EntryId, msg.Corrects)
		_, err := f.msgServer.CommitteeCorrectPosition(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("mark impaired", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeMarkImpairedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, deployed.PositionId, msg.PositionId)
		_, err := f.msgServer.CommitteeMarkImpaired(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("clear impairment", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeClearImpairmentFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, deployed.PositionId, msg.PositionId)
		_, err := f.msgServer.CommitteeClearImpairment(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("burn surplus", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeBurnSurplusFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.True(t, msg.Amount.Amount.IsPositive())
		_, err := f.msgServer.CommitteeBurnSurplus(f.ctx, msg)
		require.NoError(t, err)
	})

	// Closing last: it is where the lifecycle a deployment opened ends, and
	// every factory above needs the position still open.
	t.Run("close position", func(t *testing.T) {
		_, msg := simulation.MsgCommitteeClosePositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, deployed.PositionId, msg.PositionId)
		_, err := f.msgServer.CommitteeClosePosition(f.ctx, msg)
		require.NoError(t, err)
	})
}

// TestCommitteeFactoriesSkipWithoutAnActiveMandate covers the shared guard.
// The default mandate appoints nobody, which is the state a chain launches in
// and the state a run restoring an expired appointment lands back in.
func TestCommitteeFactoriesSkipWithoutAnActiveMandate(t *testing.T) {
	tests := map[string]func(f reserveFixture) any{
		"deploy": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeDeployFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"record update": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeRecordUpdateFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"mark impaired": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeMarkImpairedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"clear impairment": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeClearImpairmentFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"close position": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeClosePositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"burn surplus": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeBurnSurplusFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"attribute return": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeAttributeReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"correct position": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeCorrectPositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"reverse return": func(f reserveFixture) any {
			_, msg := simulation.MsgCommitteeReverseReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
	}

	for name, emit := range tests {
		t.Run(name, func(t *testing.T) {
			f := newReserveFixture(t, 1_000_000)

			msg := emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), "not active")
			require.Nil(t, msg)
		})
	}
}

// TestPositionFactoriesSkipWithoutAPosition is the second guard: an
// appointment is in place but no deployment has opened anything to act on,
// which is every block of a run before the first deployment lands.
func TestPositionFactoriesSkipWithoutAPosition(t *testing.T) {
	tests := map[string]struct {
		emit   func(f reserveFixture) any
		reason string
	}{
		"record update": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCommitteeRecordUpdateFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no open position to restate",
		},
		"mark impaired": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCommitteeMarkImpairedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no sound position to impair",
		},
		"clear impairment": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCommitteeClearImpairmentFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no impaired position to clear",
		},
		"close position": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCommitteeClosePositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no open position to close",
		},
		"attribute return": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCommitteeAttributeReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no open position to attribute a return to",
		},
		"correct position": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCommitteeCorrectPositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no deployment entry on an open position to correct",
		},
		"reverse return": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCommitteeReverseReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no unreversed return attribution on an open position",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newReserveFixture(t, 1_000_000, withActiveCommittee())

			msg := tc.emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), tc.reason)
			require.Nil(t, msg)
		})
	}
}

// TestMsgCommitteeDeployFactorySkipsWithoutHeadroom pins the allowance guard.
// A deployment is the only committee message bounded by the mandate rather
// than by a position, and an empty Reserve is the state a run spends its
// opening blocks in.
func TestMsgCommitteeDeployFactorySkipsWithoutHeadroom(t *testing.T) {
	f := newReserveFixture(t, 0, withActiveCommittee())

	_, msg := simulation.MsgCommitteeDeployFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "no headroom")
	require.Nil(t, msg)
}

// TestMsgCommitteeBurnSurplusFactorySkipsWithoutSurplus covers the other side
// of the burn: a requirement that claims everything recognised leaves nothing
// to destroy, which is the common case rather than the exception.
func TestMsgCommitteeBurnSurplusFactorySkipsWithoutSurplus(t *testing.T) {
	f := newReserveFixture(t, 1_000_000, withActiveCommittee(), withTreasuryRequirement(math.NewInt(10_000_000)))

	_, msg := simulation.MsgCommitteeBurnSurplusFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "no burnable surplus")
	require.Nil(t, msg)
}

// TestMsgCommitteeBurnSurplusFactorySkipsWithoutATreasuryReader holds the
// wiring failure to a skip rather than a panic: Treasury injects Reserve, so
// the reader arrives after construction and a run that reached the factory
// first must not take the process down.
func TestMsgCommitteeBurnSurplusFactorySkipsWithoutATreasuryReader(t *testing.T) {
	f := newReserveFixture(t, 1_000_000, withActiveCommittee())

	_, msg := simulation.MsgCommitteeBurnSurplusFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "get burnable surplus")
	require.Nil(t, msg)
}

// TestGenReserveMandateAppointsFromTheRunsAccounts covers the generator the
// fixtures above rely on. An appointment naming an address the run does not
// hold, or a window that opens after the run ends, leaves the whole committee
// surface unsigned for the life of the simulation.
func TestGenReserveMandateAppointsFromTheRunsAccounts(t *testing.T) {
	f := newReserveFixture(t, 0)
	addresses := f.addresses()

	mandate := simulation.GenReserveMandate(f.rand, addresses)

	require.Contains(t, addresses, mandate.Committee)
	require.NotEmpty(t, mandate.Destinations)
	require.NotContains(t, mandate.Destinations, mandate.Committee, "the committee must not pay itself")
	for _, destination := range mandate.Destinations {
		require.Contains(t, addresses, destination)
	}
	require.True(t, mandate.IsActive(1))
	require.True(t, mandate.DeploymentAllowance.Amount.IsPositive())
	require.NoError(t, mandate.Validate())
}

// A run with too few accounts to separate committee from destination falls
// back to the shipped default, which appoints nobody.
func TestGenReserveMandateFallsBackWithoutEnoughAccounts(t *testing.T) {
	f := newReserveFixture(t, 0)

	require.Equal(t, types.DefaultReserveMandate(), simulation.GenReserveMandate(f.rand, nil))
	require.Equal(t, types.DefaultReserveMandate(), simulation.GenReserveMandate(f.rand, f.addresses()[:1]))
}
