package keeper

import (
	"context"
	"fmt"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

// GroupVotesByDenom groups all oracle votes for the period by denom. Inactive or jailed validators
// are filtered out, and abstain votes are given zero vote power.
func (k Keeper) GroupVotesByDenom(ctx context.Context, validatorScoreMap map[string]types.ValidatorScore) (map[string]types.DenomVotes, error) {
	votes := make(map[string]types.DenomVotes)
	iteratorHandler := func(voterAddr sdk.ValAddress, vote types.Vote) (bool, error) {
		if score, ok := validatorScoreMap[vote.Voter]; ok {
			for _, er := range vote.ExchangeRates {
				power := score.Power
				if !er.Rate.IsPositive() {
					// Make the power of abstain vote zero
					power = 0
				}

				votes[er.Denom] = append(votes[er.Denom],
					types.NewDenomVote(
						er.Rate,
						er.Denom,
						voterAddr,
						power,
					),
				)
			}
		}

		return false, nil
	}

	if err := k.Vote.Walk(ctx, nil, iteratorHandler); err != nil {
		return nil, fmt.Errorf("iterating: %w", err)
	}

	// sort grouped votes
	for denom, denomVotes := range votes {
		sort.Sort(denomVotes)
		votes[denom] = denomVotes
	}

	return votes, nil
}

// ClearVotes clears all tallied prevotes and votes from the store
func (k Keeper) ClearVotes(ctx context.Context, votePeriod uint64) error {
	// Clear all prevotes
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.Prevote.Walk(
		ctx,
		nil,
		func(voterAddr sdk.ValAddress, prevote types.Prevote) (bool, error) {
			if sdkCtx.BlockHeight() > int64(prevote.SubmitBlock+votePeriod) {
				if err := k.Prevote.Remove(ctx, voterAddr); err != nil {
					return false, err
				}
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	// Clear all aggregate votes
	if err := k.Vote.Walk(
		ctx,
		nil,
		func(voterAddr sdk.ValAddress, _ types.Vote) (bool, error) {
			if err := k.Vote.Remove(ctx, voterAddr); err != nil {
				return false, err
			}
			return false, nil
		},
	); err != nil {
		return err
	}

	return nil
}
