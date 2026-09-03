package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"

	"github.com/ararat-network/ark/app"
	securitykeeper "github.com/ararat-network/ark/x/security/keeper"
	securitytypes "github.com/ararat-network/ark/x/security/types"
)

// securityFixture is a booted chain plus the one appointment the tests act
// under. It is lighter than the oracle-driven fixtures elsewhere in this
// package because no security power touches prices.
type securityFixture struct {
	app       *app.ArkApp
	ctx       sdk.Context
	msgServer securitytypes.MsgServer
	authority string
	committee string
	term      uint64
}

// newSecurityFixture boots a chain and appoints a committee through the real
// governance message.
func newSecurityFixture(t *testing.T) *securityFixture {
	t.Helper()

	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})

	f := &securityFixture{
		app:       arkApp,
		ctx:       ctx,
		msgServer: securitykeeper.NewMsgServerImpl(arkApp.SecurityKeeper),
		authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		// Account addresses are exactly 20 bytes; the committee is validated as
		// a canonical account address on the way in.
		committee: sdk.AccAddress([]byte("security-committee..")).String(),
	}

	_, err := f.msgServer.SetSecurityMandate(ctx, &securitytypes.MsgSetSecurityMandate{
		Authority:        f.authority,
		Committee:        f.committee,
		ActivationHeight: uint64(ctx.BlockHeight()),
		ExpiryHeight:     uint64(ctx.BlockHeight()) + 1_000,
	})
	require.NoError(t, err)

	appointment, err := arkApp.SecurityKeeper.Mandate.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, f.committee, appointment.Committee)
	require.NotZero(t, appointment.Term)
	f.term = appointment.Term

	return f
}

// TestSecurityCommitteeUpgradeSlot drives the upgrade powers through real
// wiring. Only provable here: a committee-signed message reaches x/upgrade at
// all, carrying an authority that module re-validates for itself.
func TestSecurityCommitteeUpgradeSlot(t *testing.T) {
	f := newSecurityFixture(t)
	upgradeMsgServer := upgradekeeper.NewMsgServerImpl(f.app.UpgradeKeeper)

	// The committee fills an empty slot, and x/upgrade holds the plan it built.
	_, err := f.msgServer.CommitteePlanUpgrade(f.ctx, &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    f.committee,
		ExpectedTerm: f.term,
		Name:         "v2-emergency",
		Height:       f.ctx.BlockHeight() + 500,
		Info:         "patched binaries",
	})
	require.NoError(t, err)

	plan, err := f.app.UpgradeKeeper.GetUpgradePlan(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "v2-emergency", plan.Name)

	record, err := f.app.SecurityKeeper.CommitteePlan.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, record.Matches(plan.Name, plan.Height))

	// Its own plan may be replaced: the slot already belongs to the committee.
	_, err = f.msgServer.CommitteePlanUpgrade(f.ctx, &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    f.committee,
		ExpectedTerm: f.term,
		Name:         "v2-emergency-b",
		Height:       f.ctx.BlockHeight() + 600,
	})
	require.NoError(t, err)

	plan, err = f.app.UpgradeKeeper.GetUpgradePlan(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "v2-emergency-b", plan.Name)

	// Governance overwrites the slot, so the record now disagrees with what is
	// pending — how the module learns the plan is no longer its own.
	_, err = upgradeMsgServer.SoftwareUpgrade(f.ctx, &upgradetypes.MsgSoftwareUpgrade{
		Authority: f.authority,
		Plan: upgradetypes.Plan{
			Name:   "v3-planned",
			Height: f.ctx.BlockHeight() + 900,
		},
	})
	require.NoError(t, err)

	// A governance plan is out of reach both ways: the committee can neither
	// schedule over it nor cancel it to clear the slot first.
	_, err = f.msgServer.CommitteePlanUpgrade(f.ctx, &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    f.committee,
		ExpectedTerm: f.term,
		Name:         "v2-emergency-c",
		Height:       f.ctx.BlockHeight() + 700,
	})
	require.ErrorContains(t, err, "only governance replaces it")

	_, err = f.msgServer.CommitteeCancelUpgrade(f.ctx, &securitytypes.MsgCommitteeCancelUpgrade{
		Committee:    f.committee,
		ExpectedTerm: f.term,
	})
	require.ErrorContains(t, err, "only governance cancels it")

	plan, err = f.app.UpgradeKeeper.GetUpgradePlan(f.ctx)
	require.NoError(t, err)
	require.Equal(t, "v3-planned", plan.Name)

	// Governance clears its own plan, and the committee can act again.
	_, err = upgradeMsgServer.CancelUpgrade(f.ctx, &upgradetypes.MsgCancelUpgrade{
		Authority: f.authority,
	})
	require.NoError(t, err)

	_, err = f.msgServer.CommitteePlanUpgrade(f.ctx, &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    f.committee,
		ExpectedTerm: f.term,
		Name:         "v2-emergency-d",
		Height:       f.ctx.BlockHeight() + 800,
	})
	require.NoError(t, err)

	_, err = f.msgServer.CommitteeCancelUpgrade(f.ctx, &securitytypes.MsgCommitteeCancelUpgrade{
		Committee:    f.committee,
		ExpectedTerm: f.term,
	})
	require.NoError(t, err)

	_, err = f.app.UpgradeKeeper.GetUpgradePlan(f.ctx)
	require.ErrorIs(t, err, upgradetypes.ErrNoUpgradePlanFound)
}

// TestSecurityCommitteeTermRotation pins that replacing an appointment
// invalidates transactions prepared against the outgoing one, and that
// disabling the mandate closes the fast path entirely.
func TestSecurityCommitteeTermRotation(t *testing.T) {
	f := newSecurityFixture(t)
	staleTerm := f.term

	// Governance re-appoints the same account, which still advances the term.
	_, err := f.msgServer.SetSecurityMandate(f.ctx, &securitytypes.MsgSetSecurityMandate{
		Authority:        f.authority,
		Committee:        f.committee,
		ActivationHeight: uint64(f.ctx.BlockHeight()),
		ExpiryHeight:     uint64(f.ctx.BlockHeight()) + 1_000,
	})
	require.NoError(t, err)

	_, err = f.msgServer.CommitteePlanUpgrade(f.ctx, &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    f.committee,
		ExpectedTerm: staleTerm,
		Name:         "v2-emergency",
		Height:       f.ctx.BlockHeight() + 500,
	})
	require.ErrorContains(t, err, "term mismatch")

	_, err = f.app.UpgradeKeeper.GetUpgradePlan(f.ctx)
	require.ErrorIs(t, err, upgradetypes.ErrNoUpgradePlanFound)

	// Disabling retains the term and leaves no committee able to act.
	_, err = f.msgServer.SetSecurityMandate(f.ctx, &securitytypes.MsgSetSecurityMandate{
		Authority: f.authority,
	})
	require.NoError(t, err)

	appointment, err := f.app.SecurityKeeper.Mandate.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, appointment.IsDisabled())

	_, err = f.msgServer.CommitteePlanUpgrade(f.ctx, &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    f.committee,
		ExpectedTerm: appointment.Term,
		Name:         "v2-emergency",
		Height:       f.ctx.BlockHeight() + 500,
	})
	require.ErrorContains(t, err, "signer is not the exact appointed committee")
}

// TestSecurityCommitteeRefusesAStranger pins end to end that the envelope is
// the whole gate: an account that is not the appointed committee reaches no
// upstream module, whatever it sends.
func TestSecurityCommitteeRefusesAStranger(t *testing.T) {
	f := newSecurityFixture(t)
	stranger := sdk.AccAddress([]byte("not-the-committee...")).String()

	_, err := f.msgServer.CommitteePlanUpgrade(f.ctx, &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    stranger,
		ExpectedTerm: f.term,
		Name:         "v2-emergency",
		Height:       f.ctx.BlockHeight() + 500,
	})
	require.ErrorContains(t, err, "signer is not the exact appointed committee")

	_, err = f.msgServer.CommitteeRecoverClient(f.ctx, &securitytypes.MsgCommitteeRecoverClient{
		Committee:          stranger,
		ExpectedTerm:       f.term,
		SubjectClientId:    "07-tendermint-0",
		SubstituteClientId: "07-tendermint-1",
	})
	require.ErrorContains(t, err, "signer is not the exact appointed committee")

	// Nothing reached x/upgrade: the envelope refuses before dispatch.
	_, err = f.app.UpgradeKeeper.GetUpgradePlan(f.ctx)
	require.ErrorIs(t, err, upgradetypes.ErrNoUpgradePlanFound)
}
