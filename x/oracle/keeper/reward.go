package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

// RewardVoteWinners will give out a portion of seigniorage reward (rewardWeight) to the
// oracle voters that voted faithfully at the end of every VotePeriod.
func (k Keeper) RewardVoteWinners(
	ctx context.Context,
	votePeriod,
	rewardDistributionWindow uint64,
	validatorScores map[string]types.ValidatorScore,
) error {
	// sum weight of the scores
	votePowerSum := int64(0)
	for _, score := range validatorScores {
		votePowerSum += score.Weight
	}

	// return if there are no votes
	if votePowerSum == 0 {
		k.Logger(ctx).Info("no votes for this period", "votePeriod", votePeriod)
		return nil
	}

	rewardAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	rewardPool := k.bankKeeper.GetAllBalances(ctx, rewardAcc.GetAddress())

	// return if there's no rewards to give out
	if rewardPool.IsZero() {
		k.Logger(ctx).Info("no rewards for this period", "votePeriod", votePeriod)
		return nil
	}

	// rewardCoin  = oraclePool * VotePeriod / RewardDistributionWindow
	periodRewards := math.LegacyNewDecFromInt(rewardPool.AmountOf(core.MicroArkDenom)).
		MulInt64(int64(votePeriod)).
		QuoInt64(int64(rewardDistributionWindow))

	// distribute rewards
	var distributedReward sdk.Coins
	for _, score := range validatorScores {
		rewardAmt := periodRewards.QuoInt64(votePowerSum).MulInt64(score.Weight).TruncateInt()
		rewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, rewardAmt))
		if rewardCoins.IsZero() {
			continue
		}

		validator, err := k.stakingKeeper.Validator(ctx, score.Recipient)
		if err != nil {
			return fmt.Errorf("getting validator %s for oracle rewards: %w", score.Recipient, err)
		}
		if validator == nil {
			return fmt.Errorf("validator not found for oracle rewards: %s", score.Recipient)
		}

		if err := k.distrKeeper.AllocateTokensToValidator(ctx, validator, sdk.NewDecCoinsFromCoins(rewardCoins...)); err != nil {
			return fmt.Errorf(
				"allocating oracle rewards to %s with reward %s and weight %d: %w",
				score.Recipient,
				rewardCoins.String(),
				score.Weight,
				err,
			)
		}
		distributedReward = distributedReward.Add(rewardCoins...)
	}

	// Move distributed reward to distribution module
	err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.distributionName, distributedReward)
	if err != nil {
		return fmt.Errorf("sending coins to distribution module: %w", err)
	}

	return nil
}
