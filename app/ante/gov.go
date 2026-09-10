package ante

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/ararat-network/ark/app/mempool"
)

// GovernancePrivilege checks the stake or deposit prerequisites for priority.
// Ordinary refusals leave the transaction in the normal lane. Handlers still
// enforce execution validity, including prerequisites set by earlier messages.
func GovernancePrivilege(staking *stakingkeeper.Keeper, bank bankkeeper.BaseKeeper, gov *govkeeper.Keeper) mempool.Privilege {
	return mempool.Privilege{
		Lane: mempool.LaneGovernance,
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
		Vouch: func(ctx sdk.Context, msg sdk.Msg) (bool, error) {
			switch m := msg.(type) {
			case *govv1.MsgVote:
				return vouchGovernanceVote(ctx, staking, gov, m.ProposalId, msg)
			case *govv1.MsgVoteWeighted:
				return vouchGovernanceVote(ctx, staking, gov, m.ProposalId, msg)
			case *govv1beta1.MsgVote:
				return vouchGovernanceVote(ctx, staking, gov, m.ProposalId, msg)
			case *govv1beta1.MsgVoteWeighted:
				return vouchGovernanceVote(ctx, staking, gov, m.ProposalId, msg)
			case *govv1.MsgSubmitProposal:
				return vouchSubmitProposal(ctx, bank, gov, m.Proposer, m.InitialDeposit, m.Expedited)
			case *govv1beta1.MsgSubmitProposal:
				return vouchSubmitProposal(ctx, bank, gov, m.Proposer, m.InitialDeposit, false)
			case *govv1.MsgDeposit:
				return vouchDeposit(ctx, bank, gov, m.ProposalId, m.Depositor, m.Amount)
			case *govv1beta1.MsgDeposit:
				return vouchDeposit(ctx, bank, gov, m.ProposalId, m.Depositor, m.Amount)
			case *govv1.MsgCancelProposal:
				return vouchCancelProposal(ctx, gov, m.ProposalId, m.Proposer)
			default:
				return false, errortypes.ErrInvalidRequest.Wrapf("%s is not a governance message", sdk.MsgTypeURL(msg))
			}
		},
	}
}

func vouchGovernanceVote(ctx sdk.Context, staking *stakingkeeper.Keeper, gov *govkeeper.Keeper, proposalID uint64, msg sdk.Msg) (bool, error) {
	proposal, found, err := findProposal(ctx, gov, proposalID)
	if err != nil || !found {
		return false, err
	}
	if proposal.Status != govv1.StatusVotingPeriod || proposal.VotingEndTime == nil || !ctx.BlockTime().Before(*proposal.VotingEndTime) {
		return false, nil
	}
	if checked, _ := ctx.Value(votesCheckedKey{}).(bool); checked {
		return true, nil
	}
	// Direct vouch callers may not have run the ante vote policy. Only its
	// ordinary refusals mean loss of priority.
	err = vouchVote(ctx, staking, msg)
	if errors.Is(err, errortypes.ErrUnauthorized) || errors.Is(err, errortypes.ErrInvalidAddress) {
		return false, nil
	}
	return err == nil, err
}

// vouchSubmitProposal checks x/gov's initial and per-deposit ratios before
// affordability, using the same rounding as the SDK's private helpers.
func vouchSubmitProposal(ctx sdk.Context, bank bankkeeper.BaseKeeper, gov *govkeeper.Keeper, proposer string, deposit sdk.Coins, expedited bool) (bool, error) {
	addr, err := sdk.AccAddressFromBech32(proposer)
	if err != nil {
		return false, nil
	}
	params, err := gov.Params.Get(ctx)
	if err != nil {
		return false, err
	}
	eligible, err := initialDepositEligible(params, deposit, expedited)
	if err != nil || !eligible {
		return false, err
	}
	if !depositDenomsEligible(params, deposit) {
		return false, nil
	}
	eligible, err = depositRatioEligible(params, minDeposit(params, expedited), deposit)
	if err != nil || !eligible {
		return false, err
	}
	return depositSpendable(ctx, bank, addr, deposit)
}

func vouchDeposit(ctx sdk.Context, bank bankkeeper.BaseKeeper, gov *govkeeper.Keeper, proposalID uint64, depositor string, amount sdk.Coins) (bool, error) {
	addr, err := sdk.AccAddressFromBech32(depositor)
	if err != nil {
		return false, nil
	}
	proposal, found, err := findProposal(ctx, gov, proposalID)
	if err != nil || !found {
		return false, err
	}
	if proposal.Status != govv1.StatusDepositPeriod && proposal.Status != govv1.StatusVotingPeriod {
		return false, nil
	}
	params, err := gov.Params.Get(ctx)
	if err != nil {
		return false, err
	}
	if !amount.IsValid() || !depositDenomsEligible(params, amount) {
		return false, nil
	}
	eligible, err := depositRatioEligible(params, proposal.GetMinDepositFromParams(params), amount)
	if err != nil || !eligible {
		return false, err
	}
	return depositSpendable(ctx, bank, addr, amount)
}

func vouchCancelProposal(ctx sdk.Context, gov *govkeeper.Keeper, proposalID uint64, proposer string) (bool, error) {
	proposal, found, err := findProposal(ctx, gov, proposalID)
	if err != nil || !found {
		return false, err
	}
	if proposal.Proposer == "" || proposal.Proposer != proposer {
		return false, nil
	}
	if proposal.Status != govv1.StatusDepositPeriod && proposal.Status != govv1.StatusVotingPeriod {
		return false, nil
	}
	return proposal.VotingEndTime == nil || !proposal.VotingEndTime.Before(ctx.BlockTime()), nil
}

// findProposal distinguishes ordinary absence from an unexpected store error.
func findProposal(ctx sdk.Context, gov *govkeeper.Keeper, proposalID uint64) (govv1.Proposal, bool, error) {
	proposal, err := gov.Proposals.Get(ctx, proposalID)
	if errors.Is(err, collections.ErrNotFound) {
		return govv1.Proposal{}, false, nil
	}
	if err != nil {
		return govv1.Proposal{}, false, fmt.Errorf("proposal %d: %w", proposalID, err)
	}
	return proposal, true, nil
}

func minDeposit(params govv1.Params, expedited bool) sdk.Coins {
	if expedited {
		return params.ExpeditedMinDeposit
	}
	return params.MinDeposit
}

// initialDepositEligible mirrors x/gov's initial ratio, including RoundInt.
// Build the requirement separately to avoid mutating the params' coin slice.
func initialDepositEligible(params govv1.Params, deposit sdk.Coins, expedited bool) (bool, error) {
	if !deposit.IsValid() {
		return false, nil
	}
	ratio, err := math.LegacyNewDecFromStr(params.MinInitialDepositRatio)
	if err != nil {
		return false, err
	}
	if ratio.IsZero() {
		return true, nil
	}
	minimum := minDeposit(params, expedited)
	required := make(sdk.Coins, len(minimum))
	for i, coin := range minimum {
		required[i] = sdk.NewCoin(coin.Denom, math.LegacyNewDecFromInt(coin.Amount).Mul(ratio).RoundInt())
	}
	return deposit.IsAllGTE(required), nil
}

func depositDenomsEligible(params govv1.Params, amount sdk.Coins) bool {
	for _, coin := range amount {
		if found, _ := sdk.Coins(params.MinDeposit).Find(coin.Denom); !found {
			return false
		}
	}
	return true
}

// depositRatioEligible mirrors AddDeposit: unless the ratio is zero, at
// least one accepted denomination must reach the truncated threshold.
func depositRatioEligible(params govv1.Params, minimum, amount sdk.Coins) (bool, error) {
	ratio, err := math.LegacyNewDecFromStr(params.MinDepositRatio)
	if err != nil {
		return false, err
	}
	if ratio.IsZero() {
		return true, nil
	}
	for _, coin := range minimum {
		threshold := sdk.NewCoin(coin.Denom, coin.Amount.ToLegacyDec().Mul(ratio).TruncateInt())
		if found, deposit := amount.Find(coin.Denom); found && deposit.IsGTE(threshold) {
			return true, nil
		}
	}
	return false, nil
}

// depositSpendable reads only requested denominations and excludes locked
// coins. Missing balances are zero; unexpected store errors remain errors.
// These reads do not reserve funds for pending deposits.
func depositSpendable(ctx sdk.Context, bank bankkeeper.BaseKeeper, addr sdk.AccAddress, amount sdk.Coins) (bool, error) {
	locked := bank.LockedCoins(ctx, addr)
	for _, coin := range amount {
		balance, err := bank.Balances.Get(ctx, collections.Join(addr, coin.Denom))
		if errors.Is(err, collections.ErrNotFound) {
			balance = math.ZeroInt()
		} else if err != nil {
			return false, err
		}
		unavailable := locked.AmountOf(coin.Denom)
		if balance.LT(unavailable) || balance.Sub(unavailable).LT(coin.Amount) {
			return false, nil
		}
	}
	return true, nil
}
