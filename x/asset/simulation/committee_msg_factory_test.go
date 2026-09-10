package simulation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/simulation"
	"github.com/ararat-network/ark/x/asset/types"
)

// Committee simulation requires an appointed signer whose key is in the run and whose mandate
// window is open. These tests exercise appointment generation and factory skip conditions together.

// appointCommittee installs the appointment the sim's own genesis generator
// draws, over the run's accounts.
func (f assetFixture) appointCommittee(t *testing.T) types.EmergencyMandate {
	t.Helper()

	addresses := make([]string, 0, len(f.accounts))
	for _, account := range f.accounts {
		addresses = append(addresses, account.Address.String())
	}
	mandate := simulation.GenEmergencyMandate(f.rand, addresses)
	require.NoError(t, f.keeper.EmergencyMandate.Set(f.ctx, mandate))

	return mandate
}

// TestMsgEmergencySuspendAssetFactoryEmitsASuspensionTheCommitteeMaySign checks reachable
// authorised suspension and excludes denominations already used in the current term.
func TestMsgEmergencySuspendAssetFactoryEmitsASuspensionTheCommitteeMaySign(t *testing.T) {
	f := newAssetFixture(t)
	mandate := f.appointCommittee(t)
	asset := f.seed(t, 0, types.AssetStatus_ASSET_STATUS_ACTIVE, 5)

	signers, msg := simulation.MsgEmergencySuspendAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Len(t, signers, 1)
	require.Equal(t, mandate.Committee, signers[0].Address.String(), "the appointed committee must sign")
	require.Equal(t, mandate.Committee, msg.Committee)
	require.Equal(t, mandate.Term, msg.ExpectedTerm)
	require.Equal(t, asset.Denom, msg.Denom)
	require.NoError(t, f.keeper.EmergencySuspendAsset(f.ctx, msg.Committee, msg.Denom, msg.ExpectedTerm))

	suspended, err := f.keeper.GetAsset(f.ctx, msg.Denom)
	require.NoError(t, err)
	require.Equal(t, types.AssetStatus_ASSET_STATUS_SUSPENDED, suspended.Status)
}

// A denomination the committee has already spent its one-shot power on is
// excluded rather than retried.
func TestMsgEmergencySuspendAssetFactoryExcludesASpentDenomination(t *testing.T) {
	f := newAssetFixture(t)
	f.appointCommittee(t)
	only := f.seed(t, 0, types.AssetStatus_ASSET_STATUS_ACTIVE, 5)
	require.NoError(t, f.keeper.EmergencySuspensions.Set(f.ctx, only.Denom))

	_, msg := simulation.MsgEmergencySuspendAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "no asset this term may still suspend")
	require.Nil(t, msg)
}

func TestMsgEmergencySuspendAssetFactorySkipsWithoutAnActiveMandate(t *testing.T) {
	f := newAssetFixture(t)
	f.seed(t, 0, types.AssetStatus_ASSET_STATUS_ACTIVE, 5)

	_, msg := simulation.MsgEmergencySuspendAssetFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "not active")
	require.Nil(t, msg)
}

// TestMsgSetEmergencyMandateFactory pins the appointment proposal. Drawing the
// appointee from the run's own accounts is what later makes the committee
// surface signable, and the handler refuses an appointment naming the
// authority itself.
func TestMsgSetEmergencyMandateFactory(t *testing.T) {
	f := newAssetFixture(t)

	_, msg := simulation.MsgSetEmergencyMandateFactory()(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, govAuthority(), msg.Authority)
	require.NotEqual(t, msg.Authority, msg.Committee)
	require.Less(t, msg.ActivationHeight, msg.ExpiryHeight)
	require.NoError(t, f.keeper.SetEmergencyMandate(
		f.ctx, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight,
	))
}

// TestGenEmergencyMandateAppointsFromTheRunsAccounts covers the generator the
// committee surface depends on. An appointment naming an address the run does
// not hold, or a window that closes before the run does, leaves the emergency
// route unexercised for every block of every nightly run.
func TestGenEmergencyMandateAppointsFromTheRunsAccounts(t *testing.T) {
	f := newAssetFixture(t)
	addresses := []string{f.accounts[0].Address.String()}

	mandate := simulation.GenEmergencyMandate(f.rand, addresses)

	require.Contains(t, addresses, mandate.Committee)
	require.True(t, mandate.IsActive(1))
	require.True(t, mandate.IsActive(chain.BlocksPerYear-1), "the window must outlast any run")
	require.NoError(t, mandate.Validate())
}

// With no accounts to appoint, the generator falls back to the shipped
// default, which appoints nobody.
func TestGenEmergencyMandateFallsBackWithoutAccounts(t *testing.T) {
	f := newAssetFixture(t)

	require.Equal(t, types.DefaultEmergencyMandate(), simulation.GenEmergencyMandate(f.rand, nil))
}

// TestRandomisedParamsStaysInsideTheDomain pins the parameter generator.
// Validation refuses a zero activation delay, so a generator whose range
// started there would make a share of every run's parameter proposals refusals
// the run still counts as delivered.
func TestRandomisedParamsStaysInsideTheDomain(t *testing.T) {
	f := newAssetFixture(t)

	for range 200 {
		params := simulation.RandomisedParams(f.rand)

		require.NoError(t, params.Validate())
		require.Positive(t, params.SettlementActivationDelayBlocks)
		require.LessOrEqual(t, params.SettlementActivationDelayBlocks, types.MaxSettlementActivationDelayBlocks)
	}
}
