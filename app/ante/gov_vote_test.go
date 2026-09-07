package ante_test

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

// fundVoter moves bond-denom coins from the genesis delegator — the one
// funded account on a Setup chain, reached through the validator's delegation
// record since Setup does not export it — to a fresh voter. Ark has no mint
// module account, so the SDK's minting test helper cannot fund here.
func fundVoter(tb testing.TB, arkApp *app.ArkApp, ctx sdk.Context, to sdk.AccAddress, amount math.Int) {
	tb.Helper()
	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(tb, err)
	require.NotEmpty(tb, validators)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(tb, err)
	delegations, err := arkApp.StakingKeeper.GetValidatorDelegations(ctx, valAddr)
	require.NoError(tb, err)
	require.NotEmpty(tb, delegations)

	// Delegation order follows delegator address bytes, so the genesis account
	// is found by its balance, not its position.
	coins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, amount))
	for _, delegation := range delegations {
		delegator, err := sdk.AccAddressFromBech32(delegation.DelegatorAddress)
		require.NoError(tb, err)
		if arkApp.BankKeeper.GetBalance(ctx, delegator, sdk.DefaultBondDenom).Amount.GTE(amount) {
			require.NoError(tb, arkApp.BankKeeper.SendCoins(ctx, delegator, to, coins))
			return
		}
	}
	tb.Fatalf("no delegator holds %s to fund the voter", coins)
}

// setupGovVoteTest returns a chain with two voters: one delegated past the
// stake floor, and one with no stake at all.
func setupGovVoteTest(t *testing.T) (*app.ArkApp, sdk.Context, sdk.AccAddress, sdk.AccAddress) {
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

func TestGovVoteStakeFloor(t *testing.T) {
	arkApp, ctx, rich, poor := setupGovVoteTest(t)

	wrap := func(msg sdk.Msg, depth int) sdk.Msg {
		for range depth {
			exec := authz.NewMsgExec(poor, []sdk.Msg{msg})
			msg = &exec
		}
		return msg
	}

	tests := []struct {
		name    string
		msg     sdk.Msg
		wantErr string
	}{
		{
			name: "staked voter passes",
			msg:  vote(rich),
		},
		{
			name:    "unstaked voter is refused",
			msg:     vote(poor),
			wantErr: "voting requires at least",
		},
		{
			name:    "under-staked genesis bond is refused",
			msg:     vote(poor),
			wantErr: "voting requires at least",
		},
		{
			name:    "weighted vote is checked",
			msg:     govv1.NewMsgVoteWeighted(poor, 1, govv1.WeightedVoteOptions{}, ""),
			wantErr: "voting requires at least",
		},
		{
			name:    "legacy vote is checked",
			msg:     govv1beta1.NewMsgVote(poor, 1, govv1beta1.OptionYes),
			wantErr: "voting requires at least",
		},
		{
			name: "non-vote message passes untouched",
			msg:  &govv1.MsgDeposit{Depositor: poor.String()},
		},
		{
			name:    "authz-wrapped vote is refused",
			msg:     wrap(vote(poor), 1),
			wantErr: "voting requires at least",
		},
		{
			name: "authz-wrapped staked vote passes",
			msg:  wrap(vote(rich), 3),
		},
		{
			name:    "nesting past the depth bound is refused",
			msg:     wrap(vote(rich), codectypes.MaxUnpackAnyRecursionDepth),
			wantErr: "too many nested",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ante.ValidateGovVoteMsg(ctx, arkApp.AppCodec(), arkApp.StakingKeeper, tc.msg, 0)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// TestGovVoteDecoratorSimulationMatchesExecution pins simulation to block
// execution: the same refusal for an under-staked voter, and the same gas
// for one who clears the floor.
func TestGovVoteDecoratorSimulationMatchesExecution(t *testing.T) {
	arkApp, ctx, rich, poor := setupGovVoteTest(t)
	decorator := ante.NewGovVoteDecorator(arkApp.AppCodec(), arkApp.StakingKeeper)
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }

	poorTx := treasuryFeeTx{msgs: []sdk.Msg{vote(poor)}}
	_, err := decorator.AnteHandle(ctx, poorTx, true, next)
	require.ErrorContains(t, err, "voting requires at least")
	_, err = decorator.AnteHandle(ctx, poorTx, false, next)
	require.ErrorContains(t, err, "voting requires at least")

	richTx := treasuryFeeTx{msgs: []sdk.Msg{vote(rich)}}
	simulated := ctx.WithGasMeter(storetypes.NewGasMeter(1_000_000))
	_, err = decorator.AnteHandle(simulated, richTx, true, next)
	require.NoError(t, err)
	executed := ctx.WithGasMeter(storetypes.NewGasMeter(1_000_000))
	_, err = decorator.AnteHandle(executed, richTx, false, next)
	require.NoError(t, err)
	require.Positive(t, executed.GasMeter().GasConsumed())
	require.Equal(t, executed.GasMeter().GasConsumed(), simulated.GasMeter().GasConsumed())
}

// TestVoterStakeFloorBoundary pins the floor itself: a bond of exactly the
// floor passes, one base unit under it fails.
func TestVoterStakeFloorBoundary(t *testing.T) {
	arkApp, ctx, _, _ := setupGovVoteTest(t)

	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)

	delegate := func(amount math.Int) sdk.AccAddress {
		voter := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
		fundVoter(t, arkApp, ctx, voter, amount)
		_, err := arkApp.StakingKeeper.Delegate(
			ctx, voter, amount, stakingtypes.Unbonded, validators[0], true,
		)
		require.NoError(t, err)
		return voter
	}

	exact := delegate(chain.NativeBaseAmount(1))
	require.NoError(t, ante.ValidateVoterStake(ctx, arkApp.StakingKeeper, exact))

	short := delegate(chain.NativeBaseAmount(1).SubRaw(1))
	require.ErrorContains(t, ante.ValidateVoterStake(ctx, arkApp.StakingKeeper, short),
		"voting requires at least")
}

// TestZeroVoterStakeFloorAdmitsEveryone pins what a zero floor means, which the
// simulation harness relies on: off, not "any delegation clears it". An account
// holding no delegation at all has to pass, and the stake walk alone would
// refuse it — it never runs its callback, so nothing ever sets the flag.
func TestZeroVoterStakeFloorAdmitsEveryone(t *testing.T) {
	arkApp, ctx, _, poor := setupGovVoteTest(t)

	require.ErrorContains(t, ante.ValidateVoterStake(ctx, arkApp.StakingKeeper, poor),
		"voting requires at least")

	ante.SetMinVoterStake(math.LegacyZeroDec())
	t.Cleanup(func() { ante.SetMinVoterStake(math.LegacyNewDecFromInt(chain.NativeBaseAmount(1))) })

	undelegated := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	require.NoError(t, ante.ValidateVoterStake(ctx, arkApp.StakingKeeper, poor))
	require.NoError(t, ante.ValidateVoterStake(ctx, arkApp.StakingKeeper, undelegated))
}

func TestVoterStakeValidatorErrors(t *testing.T) {
	for _, name := range []string{"missing validator", "malformed validator", "malformed delegation address"} {
		t.Run(name, func(t *testing.T) {
			arkApp, ctx, voter, _ := setupGovVoteTest(t)
			validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
			require.NoError(t, err)
			addr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
			require.NoError(t, err)
			store := ctx.KVStore(arkApp.GetKey(stakingtypes.StoreKey))
			switch name {
			case "missing validator":
				store.Delete(stakingtypes.GetValidatorKey(addr))
			case "malformed validator":
				store.Set(stakingtypes.GetValidatorKey(addr), []byte{0xff})
			case "malformed delegation address":
				delegation, err := arkApp.StakingKeeper.GetDelegation(ctx, voter, addr)
				require.NoError(t, err)
				delegation.ValidatorAddress = "invalid"
				store.Set(stakingtypes.GetDelegationKey(voter, addr), arkApp.AppCodec().MustMarshal(&delegation))
			}
			err = ante.ValidateVoterStake(ctx, arkApp.StakingKeeper, voter)
			end := ctx.BlockTime().Add(time.Hour)
			require.NoError(t, arkApp.GovKeeper.Proposals.Set(ctx, 1, govv1.Proposal{
				Id: 1, Status: govv1.StatusVotingPeriod, VotingEndTime: &end,
			}))
			eligible, priorityErr := arkApp.Privileges().Vouch(ctx, vote(voter))
			require.False(t, eligible)
			if name == "missing validator" {
				require.ErrorIs(t, err, errortypes.ErrUnauthorized)
				require.NoError(t, priorityErr)
				return
			}
			require.Error(t, err)
			require.NotErrorIs(t, err, errortypes.ErrUnauthorized)
			require.EqualError(t, priorityErr, err.Error())
			if name == "malformed validator" {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				require.ErrorContains(t, err, "decoding delegated validator address")
			}
		})
	}
}
