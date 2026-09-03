package app_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

// The fixtures below twin the ones in app/ante's tests: the router seam needs
// the unexported executionPolicyRouter, so its tests live here while the
// policy's own tests moved with the policy.

// fundVoter moves bond-denom coins from the genesis delegator — the one
// funded account on a Setup chain, reached through the validator's delegation
// record since Setup does not export it — to a fresh voter.
func fundVoter(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, to sdk.AccAddress, amount math.Int) {
	t.Helper()
	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, validators)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)
	delegations, err := arkApp.StakingKeeper.GetValidatorDelegations(ctx, valAddr)
	require.NoError(t, err)
	require.NotEmpty(t, delegations)

	coins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, amount))
	for _, delegation := range delegations {
		delegator, err := sdk.AccAddressFromBech32(delegation.DelegatorAddress)
		require.NoError(t, err)
		if arkApp.BankKeeper.GetBalance(ctx, delegator, sdk.DefaultBondDenom).Amount.GTE(amount) {
			require.NoError(t, arkApp.BankKeeper.SendCoins(ctx, delegator, to, coins))
			return
		}
	}
	t.Fatalf("no delegator holds %s to fund the voter", coins)
}

// setupRouterSeamTest returns a chain with two voters: one delegated past the
// stake floor, and one with no stake at all.
func setupRouterSeamTest(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress, sdk.AccAddress) {
	t.Helper()
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: arkApp.LastBlockHeight()})

	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, validators)

	rich := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	bond := chain.NativeBaseAmount(2)
	fundVoter(t, arkApp, ctx, rich, bond)
	_, err = arkApp.StakingKeeper.Delegate(
		ctx, rich, bond, stakingtypes.Unbonded, validators[0], true,
	)
	require.NoError(t, err)

	poor := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	return arkApp, ctx, rich, poor
}

func vote(voter sdk.AccAddress) *govv1.MsgVote {
	return govv1.NewMsgVote(voter, 1, govv1.OptionYes, "")
}

// guardAddr derives a distinct valid address per output without generating a
// key per recipient.
func guardAddr(i int) sdk.AccAddress {
	a := make([]byte, 20)
	binary.BigEndian.PutUint32(a, uint32(i))
	return sdk.AccAddress(a)
}

// multiSend builds a well-formed MsgMultiSend fanning out one base unit to
// each of n recipients.
func multiSend(from sdk.AccAddress, n int) *banktypes.MsgMultiSend {
	outputs := make([]banktypes.Output, n)
	for i := range outputs {
		outputs[i] = banktypes.Output{
			Address: guardAddr(i).String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)),
		}
	}
	return &banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{{
			Address: from.String(),
			Coins:   sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(n))),
		}},
		Outputs: outputs,
	}
}

// TestTreasuryRouterEnforcesVoteStakeFloor pins the second seam: a vote
// dispatched by a contract, derived account, or interchain account meets the
// same floor before any tax or execution.
func TestTreasuryRouterEnforcesVoteStakeFloor(t *testing.T) {
	arkApp, ctx, rich, poor := setupRouterSeamTest(t)
	router := arkApp.ExecutionPolicyRouter()

	poorVote := vote(poor)
	handler := router.Handler(poorVote)
	require.NotNil(t, handler)
	_, err := handler(ctx, poorVote)
	require.ErrorContains(t, err, "voting requires at least")

	// The staked voter clears the floor; the error moves on to the vote
	// itself, which targets a proposal that does not exist.
	richVote := vote(rich)
	_, err = router.Handler(richVote)(ctx, richVote)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "voting requires at least")
}

// TestTreasuryRouterEnforcesMultiSendGuard pins the second seam: a MultiSend
// dispatched by a contract, derived account, or interchain account meets the
// same cap before any tax or execution.
func TestTreasuryRouterEnforcesMultiSendGuard(t *testing.T) {
	arkApp, ctx, _, _ := setupRouterSeamTest(t)
	router := arkApp.ExecutionPolicyRouter()
	from := guardAddr(1 << 16)

	// One past the ante package's maxMultiSendOutputs cap of 500.
	over := multiSend(from, 501)
	handler := router.Handler(over)
	require.NotNil(t, handler)
	_, err := handler(ctx, over)
	require.ErrorContains(t, err, "too many MultiSend outputs")

	// Within the cap the guard clears; whatever fails next — tax collection
	// or the unfunded send itself — is not the guard's refusal.
	within := multiSend(from, 2)
	_, err = router.Handler(within)(ctx, within)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "too many MultiSend outputs")
}
