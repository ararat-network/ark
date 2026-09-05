package ante

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/ararat-network/ark/abci/lanes"
)

// GovernancePrivilege is the priority lane's governance half: participation
// messages, each vouched for by the first refusal x/gov's handler would make.
// A vote carries the stake floor; a proposal, deposit, or cancellation must be
// one x/gov would accept and its signer can fund. The checks mirror the
// v0.54 handlers and move with them: a vouch that drifted looser would hand
// the lane back to spam, one that drifted stricter would keep a valid
// proposal out of a block.
func GovernancePrivilege(staking *stakingkeeper.Keeper, bank bankkeeper.BaseKeeper, gov *govkeeper.Keeper) lanes.Privilege {
	return lanes.Privilege{
		Msgs: []sdk.Msg{
			&govv1.MsgSubmitProposal{},
			&govv1.MsgDeposit{},
			&govv1.MsgVote{},
			&govv1.MsgVoteWeighted{},
			&govv1.MsgCancelProposal{},
			&govv1beta1.MsgSubmitProposal{},
			&govv1beta1.MsgDeposit{},
			&govv1beta1.MsgVote{},
			&govv1beta1.MsgVoteWeighted{},
		},
		Vouch: func(ctx sdk.Context, msg sdk.Msg) error {
			switch m := msg.(type) {
			case *govv1.MsgVote, *govv1.MsgVoteWeighted, *govv1beta1.MsgVote, *govv1beta1.MsgVoteWeighted:
				return vouchVote(ctx, staking, msg)
			case *govv1.MsgSubmitProposal:
				return vouchSubmitProposal(ctx, bank, gov, m.Proposer, m.InitialDeposit, m.Expedited)
			case *govv1beta1.MsgSubmitProposal:
				// The legacy handler submits every proposal unexpedited.
				return vouchSubmitProposal(ctx, bank, gov, m.Proposer, m.InitialDeposit, false)
			case *govv1.MsgDeposit:
				return vouchDeposit(ctx, bank, gov, m.ProposalId, m.Depositor, m.Amount)
			case *govv1beta1.MsgDeposit:
				return vouchDeposit(ctx, bank, gov, m.ProposalId, m.Depositor, m.Amount)
			case *govv1.MsgCancelProposal:
				return vouchCancelProposal(ctx, gov, m.ProposalId, m.Proposer)
			default:
				return errorsmod.Wrapf(errortypes.ErrInvalidRequest, "%s is not a governance message", sdk.MsgTypeURL(msg))
			}
		},
	}
}

// vouchSubmitProposal mirrors the handler's deposit checks: a valid initial
// deposit in an accepted denomination, at least the minimum initial share of
// the proposal's minimum deposit, and a proposer who can fund it.
func vouchSubmitProposal(
	ctx sdk.Context,
	bank bankkeeper.BaseKeeper,
	gov *govkeeper.Keeper,
	proposer string,
	initialDeposit sdk.Coins,
	expedited bool,
) error {
	addr, err := sdk.AccAddressFromBech32(proposer)
	if err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, err.Error())
	}
	params, err := gov.Params.Get(ctx)
	if err != nil {
		return err
	}
	if err := validateInitialDeposit(params, initialDeposit, expedited); err != nil {
		return err
	}
	if err := validateDepositDenom(params, initialDeposit); err != nil {
		return err
	}
	return requireSpendable(ctx, bank, addr, initialDeposit)
}

// vouchDeposit mirrors AddDeposit: a live proposal, an accepted denomination,
// and a depositor who can fund the amount.
func vouchDeposit(
	ctx sdk.Context,
	bank bankkeeper.BaseKeeper,
	gov *govkeeper.Keeper,
	proposalID uint64,
	depositor string,
	amount sdk.Coins,
) error {
	addr, err := sdk.AccAddressFromBech32(depositor)
	if err != nil {
		return errorsmod.Wrap(errortypes.ErrInvalidAddress, err.Error())
	}
	proposal, err := liveProposal(ctx, gov, proposalID)
	if err != nil {
		return err
	}
	if proposal.Status != govv1.StatusDepositPeriod && proposal.Status != govv1.StatusVotingPeriod {
		return errorsmod.Wrapf(govtypes.ErrInactiveProposal, "%d", proposalID)
	}
	params, err := gov.Params.Get(ctx)
	if err != nil {
		return err
	}
	if err := validateDepositDenom(params, amount); err != nil {
		return err
	}
	return requireSpendable(ctx, bank, addr, amount)
}

// vouchCancelProposal mirrors CancelProposal: the proposal's own proposer,
// while it is still in its deposit or voting period.
func vouchCancelProposal(ctx sdk.Context, gov *govkeeper.Keeper, proposalID uint64, proposer string) error {
	proposal, err := liveProposal(ctx, gov, proposalID)
	if err != nil {
		return err
	}
	if proposal.Proposer == "" {
		return govtypes.ErrInvalidProposal.Wrapf("proposal %d doesn't have proposer %s, so cannot be canceled", proposalID, proposer)
	}
	if proposal.Proposer != proposer {
		return govtypes.ErrInvalidProposer.Wrapf("invalid proposer %s", proposer)
	}
	if proposal.Status != govv1.StatusDepositPeriod && proposal.Status != govv1.StatusVotingPeriod {
		return govtypes.ErrInvalidProposal.Wrap("proposal should be in the deposit or voting period")
	}
	if proposal.VotingEndTime != nil && proposal.VotingEndTime.Before(ctx.BlockTime()) {
		return govtypes.ErrVotingPeriodEnded.Wrapf("voting period is already ended for this proposal %d", proposalID)
	}
	return nil
}

// liveProposal reads a proposal, reporting a missing one the way x/gov's
// handlers do: the store's own not-found error.
func liveProposal(ctx sdk.Context, gov *govkeeper.Keeper, proposalID uint64) (govv1.Proposal, error) {
	proposal, err := gov.Proposals.Get(ctx, proposalID)
	if err != nil {
		return govv1.Proposal{}, fmt.Errorf("proposal %d: %w", proposalID, err)
	}
	return proposal, nil
}

// validateInitialDeposit is x/gov's rule of the same name: the initial
// deposit covers the minimum initial ratio of the minimum deposit, expedited
// or not, and a zero ratio asks nothing.
func validateInitialDeposit(params govv1.Params, initialDeposit sdk.Coins, expedited bool) error {
	if !initialDeposit.IsValid() || initialDeposit.IsAnyNegative() {
		return errorsmod.Wrap(errortypes.ErrInvalidCoins, initialDeposit.String())
	}
	ratio, err := math.LegacyNewDecFromStr(params.MinInitialDepositRatio)
	if err != nil {
		return err
	}
	if ratio.IsZero() {
		return nil
	}
	minDeposit := params.MinDeposit
	if expedited {
		minDeposit = params.ExpeditedMinDeposit
	}
	required := make(sdk.Coins, len(minDeposit))
	for i, coin := range minDeposit {
		required[i] = sdk.NewCoin(coin.Denom, math.LegacyNewDecFromInt(coin.Amount).Mul(ratio).RoundInt())
	}
	if !initialDeposit.IsAllGTE(required) {
		return errorsmod.Wrapf(govtypes.ErrMinDepositTooSmall, "was (%s), need (%s)", initialDeposit, required)
	}
	return nil
}

// validateDepositDenom is x/gov's rule of the same name: every deposited
// denomination is one the minimum deposit names.
func validateDepositDenom(params govv1.Params, amount sdk.Coins) error {
	accepted := make(map[string]struct{}, len(params.MinDeposit))
	denoms := make([]string, 0, len(params.MinDeposit))
	for _, coin := range params.MinDeposit {
		accepted[coin.Denom] = struct{}{}
		denoms = append(denoms, coin.Denom)
	}
	for _, coin := range amount {
		if _, ok := accepted[coin.Denom]; !ok {
			return errorsmod.Wrapf(
				govtypes.ErrInvalidDepositDenom,
				"deposited %s, but gov accepts only the following denom(s): %v",
				amount,
				denoms,
			)
		}
	}
	return nil
}

// requireSpendable refuses a deposit its signer cannot fund, which is where
// the handler's own transfer would fail.
func requireSpendable(ctx sdk.Context, bank bankkeeper.BaseKeeper, addr sdk.AccAddress, amount sdk.Coins) error {
	if !bank.SpendableCoins(ctx, addr).IsAllGTE(amount) {
		return errorsmod.Wrapf(errortypes.ErrInsufficientFunds, "%s cannot fund a deposit of %s", addr, amount)
	}
	return nil
}
