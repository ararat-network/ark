package ante

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// maxVoterDelegations bounds the stake walk: past this many delegations
// the voter counts as under-staked rather than the check unbounded.
const maxVoterDelegations = 100

// minVoterStake is the stake floor for casting a governance vote, the spam
// defence the Hub adopted after zero-stake accounts blanketed proposals with
// vote transactions. Compiled in like the priority lane set: it decides what
// enters blocks, so a node-local knob would fragment mempools.
//
// One NOAH, mirroring the Hub's one ATOM. The floor never binds for
// validators or for delegators inheriting their validator's vote; it falls
// entirely on delegators overriding that vote, so it is sized to be trivial
// for any individual delegator. That condition is the policy, not the
// constant: if a whole NOAH ever becomes a serious position, "1" has expired.
var minVoterStake = math.LegacyNewDecFromInt(chain.NativeBaseAmount(1))

// SetMinVoterStake overrides the floor, and zero disables the check outright.
// It exists for the simulation harness, which picks its voters at random and
// cannot be told to stake them; the Hub zeroes its own floor for the same
// reason. Never call it outside a test.
func SetMinVoterStake(stake math.LegacyDec) {
	minVoterStake = stake
}

// ValidateGovVoteMsg refuses a governance vote whose voter stakes less than
// minVoterStake, recursing through authz MsgExec so a wrapped vote cannot
// slip past either seam that calls this: the ante for signed transactions,
// and the policy router for messages a contract, derived account, or
// interchain account dispatches. Votes a passed proposal executes are the one
// path around it, and need no filter: they already won a governance vote.
// Non-vote messages pass untouched.
func ValidateGovVoteMsg(ctx sdk.Context, cdc codec.Codec, staking *stakingkeeper.Keeper, msg sdk.Msg, depth int) error {
	return walkAuthzExec(cdc, msg, depth, func(msg sdk.Msg) error {
		return vouchVote(ctx, staking, msg)
	})
}

// vouchVote applies the stake floor to one vote message; any other message
// passes untouched.
func vouchVote(ctx sdk.Context, staking *stakingkeeper.Keeper, msg sdk.Msg) error {
	var voter string
	switch vote := msg.(type) {
	case *govv1.MsgVote:
		voter = vote.Voter
	case *govv1.MsgVoteWeighted:
		voter = vote.Voter
	case *govv1beta1.MsgVote:
		voter = vote.Voter
	case *govv1beta1.MsgVoteWeighted:
		voter = vote.Voter
	default:
		return nil
	}
	addr, err := sdk.AccAddressFromBech32(voter)
	if err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, err.Error())
	}
	return validateVoterStake(ctx, staking, addr)
}

// validateVoterStake sums the voter's staked tokens across at most
// maxVoterDelegations delegations, stopping early once the floor is met. A
// delegation whose validator cannot be resolved counts nothing: skipping is
// the conservative direction, and an ante error only rejects the transaction.
func validateVoterStake(ctx sdk.Context, staking *stakingkeeper.Keeper, voter sdk.AccAddress) error {
	// A zero floor is off, not "any delegation clears it": an account holding
	// no delegation at all has to pass too, and the walk below never would.
	if minVoterStake.IsZero() {
		return nil
	}

	staked := math.LegacyZeroDec()
	enough := false
	checked := 0
	err := staking.IterateDelegatorDelegations(ctx, voter, func(delegation stakingtypes.Delegation) bool {
		valAddr, err := sdk.ValAddressFromBech32(delegation.ValidatorAddress)
		if err != nil {
			return false
		}
		validator, err := staking.GetValidator(ctx, valAddr)
		if err != nil {
			return false
		}
		staked = staked.Add(validator.TokensFromSharesTruncated(delegation.Shares))
		if staked.GTE(minVoterStake) {
			enough = true
			return true
		}
		checked++
		return checked >= maxVoterDelegations
	})
	if err != nil {
		return err
	}
	if !enough {
		return errorsmod.Wrapf(
			errortypes.ErrUnauthorized,
			"voting requires at least %s staked, have %s",
			minVoterStake,
			staked,
		)
	}
	return nil
}
