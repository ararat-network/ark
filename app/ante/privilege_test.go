package ante_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	"github.com/ararat-network/ark/app/mempool"
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
	for _, tc := range []struct {
		name     string
		ctx      sdk.Context
		msg      sdk.Msg
		eligible bool
		wantErr  string
	}{
		{"appointed committee", ctx, planUpgrade(committee, committeeTerm), true, ""},
		{"stranger", ctx, planUpgrade(stranger, committeeTerm), false, "not the exact appointed committee"},
		{"stale term", ctx, planUpgrade(committee, committeeTerm-1), false, "term mismatch"},
		{"expired", ctx.WithBlockHeight(ctx.BlockHeight() + 100), planUpgrade(committee, committeeTerm), false, "mandate is not active"},
		{"not yet active", ctx.WithBlockHeight(ctx.BlockHeight() - 1), planUpgrade(committee, committeeTerm), false, "mandate is not active"},
		{"unregistered", ctx, &banktypes.MsgSend{}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eligible, err := arkApp.Privileges().Vouch(tc.ctx, tc.msg)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.eligible, eligible)
		})
	}
}

func TestCommitteeVouchDisabledMandate(t *testing.T) {
	arkApp, ctx, committee := setupPrivilegeTest(t)
	require.NoError(t, arkApp.SecurityKeeper.Mandate.Set(ctx, securitytypes.SecurityMandate{Envelope: mandate.Disabled(committeeTerm)}))
	eligible, err := arkApp.Privileges().Vouch(ctx, planUpgrade(committee, committeeTerm))
	require.ErrorContains(t, err, "not the exact appointed committee")
	require.False(t, eligible)
}

func TestPrivilegeDecoratorAssignsLane(t *testing.T) {
	arkApp, ctx, committee := setupPrivilegeTest(t)
	stranger := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	decorator := ante.NewPrivilegeDecorator(arkApp.Privileges())
	for _, tc := range []struct {
		name    string
		msgs    []sdk.Msg
		lane    int8
		wantErr bool
	}{
		{"committee", []sdk.Msg{planUpgrade(committee, committeeTerm)}, mempool.LaneCommittee, false},
		{"stranger", []sdk.Msg{planUpgrade(stranger, committeeTerm)}, mempool.LaneNormal, true},
		{"authz committee", []sdk.Msg{execOf(stranger, planUpgrade(committee, committeeTerm))}, mempool.LaneNormal, false},
		{"authz stranger", []sdk.Msg{execOf(stranger, planUpgrade(stranger, committeeTerm))}, mempool.LaneNormal, false},
		{"mixed stranger", []sdk.Msg{planUpgrade(stranger, committeeTerm), &banktypes.MsgSend{}}, mempool.LaneNormal, false},
		{"mixed classes", []sdk.Msg{planUpgrade(stranger, committeeTerm), &govv1.MsgDeposit{}}, mempool.LaneNormal, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, simulate := range []bool{false, true} {
				reached := false
				got, err := decorator.AnteHandle(ctx, treasuryFeeTx{msgs: tc.msgs}, simulate, passThrough(t, &reached))
				if tc.wantErr {
					require.ErrorContains(t, err, "not the exact appointed committee")
					require.False(t, reached)
					continue
				}
				require.NoError(t, err)
				require.True(t, reached)
				require.Equal(t, tc.lane, laneOf(got))
			}
		})
	}
}

// A deposit can activate voting for the next message. Priority checks see the
// pre-execution state, so refusing priority must not reject this valid batch.
func TestGovernanceDepositThenVoteUsesNormalLane(t *testing.T) {
	for _, simulate := range []bool{false, true} {
		t.Run(fmt.Sprintf("simulate=%t", simulate), func(t *testing.T) {
			arkApp, ctx, proposer, _ := setupGovVoteTest(t)
			params, err := arkApp.GovKeeper.Params.Get(ctx)
			require.NoError(t, err)
			fundVoter(t, arkApp, ctx, proposer, params.MinDeposit[0].Amount.MulRaw(2))
			proposal, err := arkApp.GovKeeper.SubmitProposal(ctx, nil, "", "title", "summary", proposer, false)
			require.NoError(t, err)
			proposalID := proposal.Id
			deposit := &govv1.MsgDeposit{ProposalId: proposalID, Depositor: proposer.String(), Amount: params.MinDeposit}
			vote := &govv1.MsgVote{ProposalId: proposalID, Voter: proposer.String(), Option: govv1.OptionYes}
			tx := treasuryFeeTx{msgs: []sdk.Msg{deposit, vote}}
			set := arkApp.Privileges()
			for _, msg := range tx.GetMsgs() {
				require.Contains(t, set.URLs(), sdk.MsgTypeURL(msg), "both messages are governance candidates by type")
			}
			eligible, err := set.Vouch(ctx, deposit)
			require.NoError(t, err)
			require.True(t, eligible)
			eligible, err = set.Vouch(ctx, vote)
			require.NoError(t, err)
			require.False(t, eligible)

			// Exercise the independent vote policy as well as classification.
			handler := sdk.ChainAnteDecorators(
				ante.NewGovVoteDecorator(arkApp.AppCodec(), arkApp.StakingKeeper),
				ante.NewPrivilegeDecorator(set),
			)
			got, err := handler(ctx, tx, simulate)
			require.NoError(t, err)
			require.Equal(t, mempool.LaneNormal, laneOf(got))
			proposal, err = arkApp.GovKeeper.Proposals.Get(got, proposalID)
			require.NoError(t, err)
			require.Equal(t, govv1.StatusDepositPeriod, proposal.Status)

			for _, msg := range tx.GetMsgs() {
				execute := arkApp.MsgServiceRouter().Handler(msg)
				require.NotNil(t, execute)
				_, err = execute(got, msg)
				require.NoError(t, err)
			}
			proposal, err = arkApp.GovKeeper.Proposals.Get(got, proposalID)
			require.NoError(t, err)
			require.Equal(t, govv1.StatusVotingPeriod, proposal.Status)
			recorded, err := arkApp.GovKeeper.Votes.Get(got, collections.Join(proposalID, proposer))
			require.NoError(t, err)
			require.Len(t, recorded.Options, 1)
			require.Equal(t, govv1.OptionYes, recorded.Options[0].Option)
		})
	}
}

// setupProposalTest returns a chain with a live proposal, a funded proposer,
// and gov parameters asking half the minimum deposit up front and a
// hundredth of it per deposit.
func setupProposalTest(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress, govv1.Params, uint64) {
	t.Helper()

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})

	params, err := arkApp.GovKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.MinInitialDepositRatio = "0.5"
	params.MinDepositRatio = "0.01"
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

// depositThreshold is the smallest single deposit AddDeposit accepts.
func depositThreshold(params govv1.Params) sdk.Coin {
	ratio := math.LegacyMustNewDecFromStr(params.MinDepositRatio)
	return sdk.NewCoin(params.MinDeposit[0].Denom, params.MinDeposit[0].Amount.ToLegacyDec().Mul(ratio).TruncateInt())
}

// legacySubmit is a v1beta1 proposal carrying the text content its handler
// demands and the vouch never reads.
func legacySubmit(t *testing.T, who sdk.AccAddress, deposit sdk.Coins) *govv1beta1.MsgSubmitProposal {
	t.Helper()
	msg, err := govv1beta1.NewMsgSubmitProposal(govv1beta1.NewTextProposal("t", "s"), deposit, who)
	require.NoError(t, err)
	return msg
}

// requireHandlerAgrees runs msg through x/gov's own handler on a discarded
// branch and requires it to accept exactly when the vouch did, so the copy
// in gov.go cannot drift from the handler unseen. Errors are not compared:
// the vouch is the bond alone, and a refusal the handler makes that the
// vouch does not is only ever one that costs nothing to satisfy.
func requireHandlerAgrees(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, msg sdk.Msg, eligible bool) {
	t.Helper()
	handler := arkApp.MsgServiceRouter().Handler(msg)
	require.NotNil(t, handler, "no handler for %s", sdk.MsgTypeURL(msg))
	branch, _ := ctx.CacheContext()
	_, execErr := handler(branch, msg)
	require.Equal(t, execErr == nil, eligible, "vouch said %v, handler said %v", eligible, execErr)
}

// The governance vouches are x/gov's own bond rules: a deposit the signer
// can fund, in an accepted denomination, meeting the initial and per-deposit
// ratios; a live proposal for a deposit; the proposal's own proposer for a
// cancellation. Every case also runs through x/gov's handler, which has to
// agree, so each is complete in the fields the vouch ignores: metadata on a
// v1 proposal, content on a legacy one.
func TestGovernanceVouch(t *testing.T) {
	arkApp, ctx, proposer, params, proposalID := setupProposalTest(t)
	set := arkApp.Privileges()
	broke := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	denom := params.MinDeposit[0].Denom
	half := halfMinDeposit(params)
	short := sdk.NewCoins(sdk.NewCoin(denom, params.MinDeposit[0].Amount.QuoRaw(2).SubRaw(1)))
	foreign := sdk.NewCoins(sdk.NewInt64Coin("uatom", 1))

	submit := func(who sdk.AccAddress, deposit sdk.Coins) *govv1.MsgSubmitProposal {
		return &govv1.MsgSubmitProposal{Proposer: who.String(), InitialDeposit: deposit, Title: "t", Summary: "s", Metadata: "ipfs://vouch"}
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
			msg:  legacySubmit(t, proposer, half),
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
			name:    "deposit under the per-deposit ratio",
			msg:     &govv1.MsgDeposit{ProposalId: proposalID, Depositor: proposer.String(), Amount: sdk.NewCoins(depositThreshold(params).SubAmount(math.OneInt()))},
			wantErr: govtypes.ErrMinDepositTooSmall,
		},
		{
			name: "deposit at the per-deposit ratio",
			msg:  &govv1.MsgDeposit{ProposalId: proposalID, Depositor: proposer.String(), Amount: sdk.NewCoins(depositThreshold(params))},
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
			eligible, err := set.Vouch(ctx, tt.msg)
			require.NoError(t, err)
			require.Equal(t, tt.wantErr == nil, eligible)
			requireHandlerAgrees(t, arkApp, ctx, tt.msg, eligible)
		})
	}
}

// A zero initial-deposit ratio asks nothing of the initial deposit, as in
// x/gov, but AddDeposit's per-deposit ratio still binds on it: an empty
// deposit is refused and one at the threshold passes.
func TestGovernanceVouchZeroRatio(t *testing.T) {
	arkApp, ctx, proposer, params, _ := setupProposalTest(t)
	params.MinInitialDepositRatio = math.LegacyZeroDec().String()
	require.NoError(t, arkApp.GovKeeper.Params.Set(ctx, params))
	submit := func(deposit sdk.Coins) *govv1.MsgSubmitProposal {
		return &govv1.MsgSubmitProposal{Proposer: proposer.String(), InitialDeposit: deposit, Title: "t", Summary: "s", Metadata: "ipfs://vouch"}
	}

	empty, funded := submit(nil), submit(sdk.NewCoins(depositThreshold(params)))
	eligible, err := arkApp.Privileges().Vouch(ctx, empty)
	require.NoError(t, err)
	require.False(t, eligible)
	requireHandlerAgrees(t, arkApp, ctx, empty, eligible)
	eligible, err = arkApp.Privileges().Vouch(ctx, funded)
	require.NoError(t, err)
	require.True(t, eligible)
	requireHandlerAgrees(t, arkApp, ctx, funded, eligible)
}

func TestFundingThenDepositRemainsValid(t *testing.T) {
	arkApp, ctx, proposer, params, id := setupProposalTest(t)
	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	msgs := []sdk.Msg{
		&banktypes.MsgSend{FromAddress: proposer.String(), ToAddress: recipient.String(), Amount: halfMinDeposit(params)},
		&govv1.MsgDeposit{ProposalId: id, Depositor: recipient.String(), Amount: halfMinDeposit(params)},
	}
	got, err := ante.NewPrivilegeDecorator(arkApp.Privileges()).AnteHandle(ctx, treasuryFeeTx{msgs: msgs}, false,
		func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
	require.NoError(t, err)
	require.Equal(t, mempool.LaneNormal, laneOf(got))
	branch, _ := got.CacheContext()
	for _, msg := range msgs {
		_, err := arkApp.MsgServiceRouter().Handler(msg)(branch, msg)
		require.NoError(t, err)
	}
}

func TestDepositVouchDoesNotScanUnrelatedBalances(t *testing.T) {
	arkApp, ctx, proposer, params, id := setupProposalTest(t)
	msg := &govv1.MsgDeposit{ProposalId: id, Depositor: proposer.String(), Amount: halfMinDeposit(params)}
	measure := func() uint64 {
		metered := ctx.WithGasMeter(storetypes.NewGasMeter(1_000_000))
		eligible, err := arkApp.Privileges().Vouch(metered, msg)
		require.NoError(t, err)
		require.True(t, eligible)
		return metered.GasMeter().GasConsumed()
	}
	before := measure()
	for i := range 200 {
		require.NoError(t, arkApp.BankKeeper.Balances.Set(ctx, collections.Join(proposer, fmt.Sprintf("unrelated%03d", i)), math.OneInt()))
	}
	require.Positive(t, before)
	require.Equal(t, before, measure())
}

func TestGovernanceVotePriority(t *testing.T) {
	arkApp, ctx, rich, poor := setupGovVoteTest(t)
	ctx = ctx.WithBlockTime(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	end := ctx.BlockTime().Add(time.Hour)
	for _, tc := range []struct {
		name     string
		status   govv1.ProposalStatus
		end      *time.Time
		missing  bool
		voter    sdk.AccAddress
		eligible bool
	}{
		{"active", govv1.StatusVotingPeriod, &end, false, rich, true},
		{"understaked", govv1.StatusVotingPeriod, &end, false, poor, false},
		{"deposit period", govv1.StatusDepositPeriod, &end, false, rich, false},
		{"missing end", govv1.StatusVotingPeriod, nil, false, rich, false},
		{"missing proposal", govv1.StatusVotingPeriod, &end, true, rich, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, arkApp.GovKeeper.Proposals.Set(ctx, 1, govv1.Proposal{Id: 1, Status: tc.status, VotingEndTime: tc.end}))
			if tc.missing {
				require.NoError(t, arkApp.GovKeeper.Proposals.Remove(ctx, 1))
			}
			for _, msg := range []sdk.Msg{
				govv1.NewMsgVote(tc.voter, 1, govv1.OptionYes, ""),
				govv1.NewMsgVoteWeighted(tc.voter, 1, govv1.WeightedVoteOptions{}, ""),
				govv1beta1.NewMsgVote(tc.voter, 1, govv1beta1.OptionYes),
				govv1beta1.NewMsgVoteWeighted(tc.voter, 1, govv1beta1.WeightedVoteOptions{}),
			} {
				t.Run(sdk.MsgTypeURL(msg), func(t *testing.T) {
					eligible, err := arkApp.Privileges().Vouch(ctx, msg)
					require.NoError(t, err)
					require.Equal(t, tc.eligible, eligible)
					eligible, err = arkApp.Privileges().Vouch(ctx.WithBlockTime(end), msg)
					require.NoError(t, err)
					require.False(t, eligible, "voting end is exclusive")
				})
			}
		})
	}
}

func TestVouchPreservesUnexpectedStoreErrors(t *testing.T) {
	t.Run("mandate missing", func(t *testing.T) {
		arkApp, ctx, committee := setupPrivilegeTest(t)
		require.NoError(t, arkApp.SecurityKeeper.Mandate.Remove(ctx))
		_, err := arkApp.Privileges().Vouch(ctx, planUpgrade(committee, committeeTerm))
		require.ErrorIs(t, err, collections.ErrNotFound)
	})
	t.Run("governance parameters missing", func(t *testing.T) {
		arkApp, ctx, proposer, params, _ := setupProposalTest(t)
		require.NoError(t, arkApp.GovKeeper.Params.Remove(ctx))
		_, err := arkApp.Privileges().Vouch(ctx, &govv1.MsgSubmitProposal{Proposer: proposer.String(), InitialDeposit: halfMinDeposit(params)})
		require.ErrorIs(t, err, collections.ErrNotFound)
	})
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed ratio initial=%t", initial), func(t *testing.T) {
			arkApp, ctx, proposer, params, _ := setupProposalTest(t)
			if initial {
				params.MinInitialDepositRatio = "invalid"
			} else {
				params.MinDepositRatio = "invalid"
			}
			require.NoError(t, arkApp.GovKeeper.Params.Set(ctx, params))
			eligible, err := arkApp.Privileges().Vouch(ctx, &govv1.MsgSubmitProposal{
				Proposer: proposer.String(), InitialDeposit: halfMinDeposit(params),
			})
			require.Error(t, err, "malformed stored parameters must not become ordinary ineligibility")
			require.False(t, eligible)
		})
	}
}

// laneOf reads the lane the privilege decorator recorded in SDK context.
func laneOf(ctx sdk.Context) int8 {
	return mempool.Lane(ctx)
}
