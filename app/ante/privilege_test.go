package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/mandate"
	securitytypes "github.com/ararat-network/ark/x/security/types"
)

const committeeTerm = 3

// setupPrivilegeTest returns a chain whose security mandate appoints a
// committee for a window opening at the context height.
func setupPrivilegeTest(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress) {
	t.Helper()

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})
	height := uint64(ctx.BlockHeight())

	committee := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	require.NoError(t, arkApp.SecurityKeeper.Mandate.Set(ctx, securitytypes.SecurityMandate{
		Envelope: mandate.Envelope{
			Term:             committeeTerm,
			Committee:        committee.String(),
			ActivationHeight: height,
			ExpiryHeight:     height + 100,
		},
	}))

	return arkApp, ctx, committee
}

func planUpgrade(committee sdk.AccAddress, term uint64) *securitytypes.MsgCommitteePlanUpgrade {
	return &securitytypes.MsgCommitteePlanUpgrade{
		Committee:    committee.String(),
		ExpectedTerm: term,
		Name:         "v2",
		Height:       1_000_000,
	}
}

func execOf(grantee sdk.AccAddress, msgs ...sdk.Msg) *authz.MsgExec {
	exec := authz.NewMsgExec(grantee, msgs)
	return &exec
}

// A committee vouch is the envelope check the handler runs first: the exact
// signer, the exact term, the active window. Everything the handler would
// accept passes, and an unprivileged message is not looked at.
func TestCommitteeVouch(t *testing.T) {
	arkApp, ctx, committee := setupPrivilegeTest(t)
	stranger := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	expired := ctx.WithBlockHeight(ctx.BlockHeight() + 100)
	set := arkApp.Privileges()

	tests := []struct {
		name    string
		ctx     sdk.Context
		msg     sdk.Msg
		wantErr error
	}{
		{name: "appointed committee", ctx: ctx, msg: planUpgrade(committee, committeeTerm)},
		{name: "stranger", ctx: ctx, msg: planUpgrade(stranger, committeeTerm), wantErr: errortypes.ErrUnauthorized},
		{name: "stale term", ctx: ctx, msg: planUpgrade(committee, committeeTerm-1), wantErr: errortypes.ErrUnauthorized},
		{name: "expired window", ctx: expired, msg: planUpgrade(committee, committeeTerm), wantErr: errortypes.ErrUnauthorized},
		{
			name:    "sibling message shares the mandate",
			ctx:     ctx,
			msg:     &securitytypes.MsgCommitteeCancelUpgrade{Committee: stranger.String(), ExpectedTerm: committeeTerm},
			wantErr: errortypes.ErrUnauthorized,
		},
		{name: "unprivileged message passes untouched", ctx: ctx, msg: &banktypes.MsgSend{FromAddress: stranger.String()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := set.Vouch(tt.ctx, tt.msg)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// A disabled mandate vouches for nobody, its term retained so a transaction
// prepared under the earlier appointment stays refused.
func TestCommitteeVouchDisabledMandate(t *testing.T) {
	arkApp, ctx, committee := setupPrivilegeTest(t)
	require.NoError(t, arkApp.SecurityKeeper.Mandate.Set(ctx, securitytypes.SecurityMandate{
		Envelope: mandate.Disabled(committeeTerm),
	}))

	err := arkApp.Privileges().Vouch(ctx, planUpgrade(committee, committeeTerm))

	require.ErrorIs(t, err, errortypes.ErrUnauthorized)
}

// The decorator walks authz and refuses before its successor runs, in
// simulation too, so a gas estimate cannot pass what execution would refuse.
func TestPrivilegeDecoratorRefusesBeforeNext(t *testing.T) {
	arkApp, ctx, committee := setupPrivilegeTest(t)
	stranger := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	decorator := ante.NewPrivilegeDecorator(arkApp.AppCodec(), arkApp.Privileges())

	tests := []struct {
		name    string
		msg     sdk.Msg
		wantErr error
	}{
		{name: "committee", msg: planUpgrade(committee, committeeTerm)},
		{name: "stranger", msg: planUpgrade(stranger, committeeTerm), wantErr: errortypes.ErrUnauthorized},
		{name: "authz-wrapped committee", msg: execOf(stranger, planUpgrade(committee, committeeTerm))},
		{name: "authz-wrapped stranger", msg: execOf(stranger, planUpgrade(stranger, committeeTerm)), wantErr: errortypes.ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, simulate := range []bool{false, true} {
				var reached bool
				_, err := decorator.AnteHandle(ctx, treasuryFeeTx{msgs: []sdk.Msg{tt.msg}}, simulate, passThrough(t, &reached))
				if tt.wantErr == nil {
					require.NoError(t, err)
					require.True(t, reached)
					continue
				}
				require.ErrorIs(t, err, tt.wantErr)
				require.False(t, reached)
			}
		})
	}
}

// setupProposalTest returns a chain with a live proposal, a funded proposer,
// and gov parameters that ask for half the minimum deposit up front.
func setupProposalTest(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress, govv1.Params, uint64) {
	t.Helper()

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})

	params, err := arkApp.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.MinInitialDepositRatio = "0.5"
	require.NoError(t, arkApp.GovKeeper.Params.Set(ctx, params))

	proposer := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	fundVoter(t, arkApp, ctx, proposer, params.MinDeposit[0].Amount.MulRaw(2))
	proposal, err := arkApp.GovKeeper.SubmitProposal(ctx, nil, "", "title", "summary", proposer, false)
	require.NoError(t, err)

	return arkApp, ctx, proposer, params, proposal.Id
}

func halfMinDeposit(params govv1.Params) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(params.MinDeposit[0].Denom, params.MinDeposit[0].Amount.QuoRaw(2)))
}

// The governance vouches are x/gov's own first refusals: a deposit the
// signer can fund, in an accepted denomination, meeting the initial ratio;
// a live proposal for a deposit; the proposal's own proposer for a
// cancellation.
func TestGovernanceVouch(t *testing.T) {
	arkApp, ctx, proposer, params, proposalID := setupProposalTest(t)
	set := arkApp.Privileges()
	broke := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	denom := params.MinDeposit[0].Denom
	half := halfMinDeposit(params)
	short := sdk.NewCoins(sdk.NewCoin(denom, params.MinDeposit[0].Amount.QuoRaw(2).SubRaw(1)))
	foreign := sdk.NewCoins(sdk.NewInt64Coin("uatom", 1))

	submit := func(who sdk.AccAddress, deposit sdk.Coins) *govv1.MsgSubmitProposal {
		return &govv1.MsgSubmitProposal{Proposer: who.String(), InitialDeposit: deposit, Title: "t", Summary: "s"}
	}

	tests := []struct {
		name    string
		msg     sdk.Msg
		wantErr error
	}{
		{name: "funded proposal at the initial ratio", msg: submit(proposer, half)},
		{name: "proposal under the initial ratio", msg: submit(proposer, short), wantErr: govtypes.ErrMinDepositTooSmall},
		// The ratio check runs first, as in x/gov, so a foreign denom fails it
		// before the denom rule is reached; the deposit path below hits the rule.
		{name: "proposal in a foreign denom", msg: submit(proposer, foreign), wantErr: govtypes.ErrMinDepositTooSmall},
		{name: "proposal the proposer cannot fund", msg: submit(broke, half), wantErr: errortypes.ErrInsufficientFunds},
		{
			name: "legacy proposal",
			msg:  &govv1beta1.MsgSubmitProposal{Proposer: proposer.String(), InitialDeposit: half},
		},
		{
			name: "funded deposit on a live proposal",
			msg:  &govv1.MsgDeposit{ProposalId: proposalID, Depositor: proposer.String(), Amount: half},
		},
		{
			name:    "deposit on an unknown proposal",
			msg:     &govv1.MsgDeposit{ProposalId: proposalID + 1, Depositor: proposer.String(), Amount: half},
			wantErr: collections.ErrNotFound,
		},
		{
			name:    "deposit in a foreign denom",
			msg:     &govv1.MsgDeposit{ProposalId: proposalID, Depositor: proposer.String(), Amount: foreign},
			wantErr: govtypes.ErrInvalidDepositDenom,
		},
		{
			name:    "deposit the depositor cannot fund",
			msg:     &govv1beta1.MsgDeposit{ProposalId: proposalID, Depositor: broke.String(), Amount: half},
			wantErr: errortypes.ErrInsufficientFunds,
		},
		{
			name: "cancellation by the proposer",
			msg:  &govv1.MsgCancelProposal{ProposalId: proposalID, Proposer: proposer.String()},
		},
		{
			name:    "cancellation by a stranger",
			msg:     &govv1.MsgCancelProposal{ProposalId: proposalID, Proposer: broke.String()},
			wantErr: govtypes.ErrInvalidProposer,
		},
		{
			name:    "cancellation of an unknown proposal",
			msg:     &govv1.MsgCancelProposal{ProposalId: proposalID + 1, Proposer: proposer.String()},
			wantErr: collections.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := set.Vouch(ctx, tt.msg)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// A zero initial-deposit ratio asks nothing of the deposit itself, as in
// x/gov; the signer still has to be able to fund what they offer.
func TestGovernanceVouchZeroRatio(t *testing.T) {
	arkApp, ctx, proposer, params, _ := setupProposalTest(t)
	params.MinInitialDepositRatio = math.LegacyZeroDec().String()
	require.NoError(t, arkApp.GovKeeper.Params.Set(ctx, params))

	err := arkApp.Privileges().Vouch(ctx, &govv1.MsgSubmitProposal{
		Proposer: proposer.String(),
		Title:    "t",
		Summary:  "s",
	})

	require.NoError(t, err)
}
