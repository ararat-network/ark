package ante

import (
	"errors"
	"fmt"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// maxVoterDelegations bounds the stake walk: past this many delegations
// the voter counts as under-staked rather than the check unbounded.
const maxVoterDelegations = 100

// minVoterStake is the consensus governance-vote spam floor of one NOAH. It applies to explicit
// votes, not inherited validator votes; node-local configuration cannot change block validity.
var minVoterStake = math.LegacyNewDecFromInt(chain.NativeBaseAmount(1))

// SetMinVoterStake overrides the floor, and zero disables the check outright.
// It exists for the simulation harness, which picks its voters at random and
// cannot be told to stake them; the Hub zeroes its own floor for the same
// reason. Never call it outside a test.
func SetMinVoterStake(stake math.LegacyDec) {
	minVoterStake = stake
}

// votesCheckedKey marks the current transaction's vote policy pass, avoiding
// a second delegation walk when the priority decorator follows it.
type votesCheckedKey struct{}

type GovVoteDecorator struct {
	cdc     codec.Codec
	staking *stakingkeeper.Keeper
	gov     *govkeeper.Keeper
}

func NewGovVoteDecorator(cdc codec.Codec, staking *stakingkeeper.Keeper, gov *govkeeper.Keeper) GovVoteDecorator {
	return GovVoteDecorator{cdc: cdc, staking: staking, gov: gov}
}

// AnteHandle enforces the stake floor in every mode. At check and recheck it also refuses a vote
// or deposit x/gov would refuse for its proposal's state, so a late vote leaves the pool instead
// of being included to fail. Execution and simulation leave that to x/gov, so their gas is unchanged.
func (d GovVoteDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	mode := ctx.ExecMode()
	checking := mode == sdk.ExecModeCheck || mode == sdk.ExecModeReCheck
	guard := closedProposalGuard{gov: d.gov, deposited: map[uint64]bool{}, created: map[uint64]bool{}}
	for _, msg := range tx.GetMsgs() {
		err := walkAuthzExec(d.cdc, msg, 0, func(msg sdk.Msg) error {
			if err := vouchVote(ctx, d.staking, msg); err != nil {
				return err
			}
			if checking {
				return guard.visit(ctx, msg)
			}
			return nil
		})
		if err != nil {
			return ctx, err
		}
	}
	return next(ctx.WithValue(votesCheckedKey{}, true), tx, simulate)
}

// closedProposalGuard mirrors x/gov's refusals at check: a vote needs its proposal in the voting
// index, a deposit an existing proposal in its deposit or voting period. Check runs on committed
// state, so the guard tracks what this transaction's earlier messages change: a deposit may open
// voting for a later vote, and each submission creates the next proposal ID for a later message.
// A contract call may dispatch either, so nothing after one is refused. An earlier transaction in
// the same block can do the same, and a vote it would enable is refused here until that
// transaction commits.
type closedProposalGuard struct {
	gov       *govkeeper.Keeper
	deposited map[uint64]bool
	created   map[uint64]bool
	opaque    bool // a contract ran
}

func (g *closedProposalGuard) visit(ctx sdk.Context, msg sdk.Msg) error {
	if g.opaque {
		return nil
	}
	switch m := msg.(type) {
	case *govv1.MsgSubmitProposal, *govv1beta1.MsgSubmitProposal:
		next, err := g.gov.ProposalID.Peek(ctx)
		if err != nil {
			return err
		}
		g.created[next+uint64(len(g.created))] = true
		return nil
	case *govv1.MsgVote:
		return g.vote(ctx, m.ProposalId)
	case *govv1.MsgVoteWeighted:
		return g.vote(ctx, m.ProposalId)
	case *govv1beta1.MsgVote:
		return g.vote(ctx, m.ProposalId)
	case *govv1beta1.MsgVoteWeighted:
		return g.vote(ctx, m.ProposalId)
	case *govv1.MsgDeposit:
		return g.deposit(ctx, m.ProposalId)
	case *govv1beta1.MsgDeposit:
		return g.deposit(ctx, m.ProposalId)
	default:
		// A contract may dispatch a deposit or a submission, so nothing
		// after it is provably refusable.
		g.opaque = strings.HasPrefix(sdk.MsgTypeURL(msg), "/cosmwasm.wasm.v1.")
		return nil
	}
}

// vote admits what AddVote admits, the voting index, and a proposal this transaction created or
// deposited on, since either may open voting first.
func (g *closedProposalGuard) vote(ctx sdk.Context, id uint64) error {
	if g.created[id] || g.deposited[id] {
		return nil
	}
	voting, err := g.gov.VotingPeriodProposals.Has(ctx, id)
	if err != nil {
		return err
	}
	if !voting {
		return errorsmod.Wrapf(govtypes.ErrInactiveProposal, "%d", id)
	}
	return nil
}

// deposit admits what AddDeposit admits and a proposal this transaction created. x/gov returns
// the uncoded collections error for a missing proposal; the coded not-found reads as a refusal
// rather than an internal error.
func (g *closedProposalGuard) deposit(ctx sdk.Context, id uint64) error {
	if g.created[id] {
		g.deposited[id] = true
		return nil
	}
	proposal, found, err := findProposal(ctx, g.gov, id)
	if err != nil {
		return err
	}
	if !found {
		return errorsmod.Wrapf(errortypes.ErrNotFound, "proposal %d", id)
	}
	if proposal.Status != govv1.StatusDepositPeriod && proposal.Status != govv1.StatusVotingPeriod {
		return errorsmod.Wrapf(govtypes.ErrInactiveProposal, "%d", id)
	}
	g.deposited[id] = true
	return nil
}

// ValidateGovVoteMsg enforces minVoterStake on explicit votes, including authz leaves, at both
// signed and execution-generated message boundaries. Non-vote messages pass through. Governance
// proposal execution uses the underlying router.
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
// delegation whose validator is missing counts nothing. Malformed delegation
// addresses and unexpected validator-read errors remain state errors.
func validateVoterStake(ctx sdk.Context, staking *stakingkeeper.Keeper, voter sdk.AccAddress) error {
	// A zero floor is off, not "any delegation clears it": an account holding
	// no delegation at all has to pass too, and the walk below never would.
	if minVoterStake.IsZero() {
		return nil
	}

	staked := math.LegacyZeroDec()
	enough := false
	checked := 0
	var lookupErr error
	err := staking.IterateDelegatorDelegations(ctx, voter, func(delegation stakingtypes.Delegation) bool {
		checked++
		valAddr, err := sdk.ValAddressFromBech32(delegation.ValidatorAddress)
		if err != nil {
			lookupErr = fmt.Errorf("decoding delegated validator address %q: %w", delegation.ValidatorAddress, err)
			return true
		}
		validator, err := staking.GetValidator(ctx, valAddr)
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return checked >= maxVoterDelegations
		}
		if err != nil {
			lookupErr = fmt.Errorf("reading delegated validator %s: %w", delegation.ValidatorAddress, err)
			return true
		}
		staked = staked.Add(validator.TokensFromSharesTruncated(delegation.Shares))
		if staked.GTE(minVoterStake) {
			enough = true
			return true
		}
		return checked >= maxVoterDelegations
	})
	if err != nil {
		return err
	}
	if lookupErr != nil {
		return lookupErr
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
