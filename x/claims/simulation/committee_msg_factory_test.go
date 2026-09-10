package simulation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/x/claims/simulation"
	"github.com/ararat-network/ark/x/claims/types"
)

// Committee factories require an owned signer and an open appointment window. These tests submit
// and cancel from reachable genesis state so legitimate skips cannot hide unreachable operations.

// appointCommittee installs the appointment the sim's own genesis generator
// draws over the run's accounts.
func (f claimFixture) appointCommittee(t *testing.T) types.ClaimsMandate {
	t.Helper()

	addresses := make([]string, 0, len(f.accounts))
	for _, account := range f.accounts {
		addresses = append(addresses, account.Address.String())
	}
	mandate := simulation.GenClaimsMandate(f.rand, addresses)
	require.NoError(t, f.keeper.ClaimsMandate.Set(f.ctx, mandate))

	return mandate
}

// TestCommitteeClaimFactoriesSubmitAndWithdraw is the reachability proof. The
// cancel factory can only find a subject once the submit factory's claim has
// been delivered, so the two are exercised in that order: a cancel factory
// tested alone would skip for a reason no run could tell from a broken one.
func TestCommitteeClaimFactoriesSubmitAndWithdraw(t *testing.T) {
	f := newClaimFixture(t, 1_000_000)
	mandate := f.appointCommittee(t)

	signers, submit := simulation.MsgCommitteeSubmitClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Len(t, signers, 1)
	require.Equal(t, mandate.Committee, signers[0].Address.String(), "the appointed committee must sign")
	require.Equal(t, mandate.Term, submit.ExpectedTerm)
	require.NotEmpty(t, submit.Reference, "a claim must carry a reference")
	require.True(t, submit.Amount.Amount.IsPositive())
	require.True(t, submit.Amount.Amount.LTE(mandate.CommitteeClaimLimit.Amount))
	submitted, err := f.msgServer.CommitteeSubmitClaim(f.ctx, submit)
	require.NoError(t, err)

	_, cancel := simulation.MsgCommitteeCancelClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, submitted.ClaimId, cancel.ClaimId)
	require.Equal(t, mandate.Term, cancel.ExpectedTerm)
	_, err = f.msgServer.CommitteeCancelClaim(f.ctx, cancel)
	require.NoError(t, err)

	withdrawn, err := f.keeper.Claims.Get(f.ctx, submitted.ClaimId)
	require.NoError(t, err)
	require.Equal(t, types.ClaimStatus_CLAIM_STATUS_CANCELLED, withdrawn.Status)
}

func TestCommitteeClaimFactoriesSkipWithoutAnActiveMandate(t *testing.T) {
	tests := map[string]func(f claimFixture) any{
		"submit": func(f claimFixture) any {
			_, msg := simulation.MsgCommitteeSubmitClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
		"cancel": func(f claimFixture) any {
			_, msg := simulation.MsgCommitteeCancelClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			return msg
		},
	}

	for name, emit := range tests {
		t.Run(name, func(t *testing.T) {
			f := newClaimFixture(t, 1_000_000)

			msg := emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), "not active")
			require.Nil(t, msg)
		})
	}
}

// TestMsgCommitteeSubmitClaimFactorySkipsWithoutHeadroom pins the tighter of
// the two ceilings the factory works under. An empty Insurance is the state a
// run spends its opening blocks in.
func TestMsgCommitteeSubmitClaimFactorySkipsWithoutHeadroom(t *testing.T) {
	f := newClaimFixture(t, 0)
	f.appointCommittee(t)

	_, msg := simulation.MsgCommitteeSubmitClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "no headroom")
	require.Nil(t, msg)
}

// TestMsgCommitteeSubmitClaimFactorySkipsWhenTheClaimWouldOutliveTheMandate
// covers the window guard: a claim that closes after the appointment expires
// cannot be cancelled by the committee that raised it.
func TestMsgCommitteeSubmitClaimFactorySkipsWhenTheClaimWouldOutliveTheMandate(t *testing.T) {
	f := newClaimFixture(t, 1_000_000)
	mandate := f.appointCommittee(t)
	mandate.ExpiryHeight = uint64(f.ctx.BlockHeight()) + 1
	require.NoError(t, f.keeper.ClaimsMandate.Set(f.ctx, mandate))

	_, msg := simulation.MsgCommitteeSubmitClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "close after the appointment expires")
	require.Nil(t, msg)
}

// A committee may not withdraw a claim governance raised, so a run whose only
// pending claims came from proposals finds nothing to cancel.
func TestMsgCommitteeCancelClaimFactorySkipsWithoutACommitteeClaim(t *testing.T) {
	f := newClaimFixture(t, 1_000_000)
	f.appointCommittee(t)

	_, msg := simulation.MsgCommitteeCancelClaimFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "no pending committee claim to cancel")
	require.Nil(t, msg)
}
