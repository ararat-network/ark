package keeper

import (
	"context"
	"errors"
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
	votePeriod int64,
	rewardDistributionWindow int64,
	validatorScores map[string]types.ValidatorScore,
) error {
	// sum weight of the scores
	votePowerSum := int64(0)
	for _, score := range validatorScores {
		votePowerSum += score.Weight
	}

	// return if there are no votes
	if votePowerSum == 0 {
		return errors.New("no votes")
	}

	rewardAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	rewardPool := k.bankKeeper.GetAllBalances(ctx, rewardAcc.GetAddress())

	// return if there's no rewards to give out
	if rewardPool.IsZero() {
		return errors.New("no rewards to give out")
	}

	// rewardCoin  = oraclePool * VotePeriod / RewardDistributionWindow
	periodRewards := math.LegacyNewDecFromInt(rewardPool.AmountOf(core.MicroArkDenom)).
		MulInt64(votePeriod).
		QuoInt64(rewardDistributionWindow)

	// distribute rewards
	var distributedReward sdk.Coins
	for _, score := range validatorScores {
		validator := k.stakingKeeper.Validator(ctx, score.Recipient)
		rewardAmt := periodRewards.QuoInt64(votePowerSum).MulInt64(score.Weight).TruncateInt()
		rewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, rewardAmt))

		if validator != nil && !rewardCoins.IsZero() {
			if err := k.distrKeeper.AllocateTokensToValidator(ctx, validator, sdk.NewDecCoinsFromCoins(rewardCoins...)); err != nil {
				k.Logger(ctx).Warn(
					"failed to allocate oracle rewards",
					"validator", score.Recipient.String(),
					"reward", rewardCoins.String(),
					"weight", score.Weight,
					"error", err,
				)
				continue
			}
			distributedReward = distributedReward.Add(rewardCoins...)
		}
	}

	// Move distributed reward to distribution module
	err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.distributionName, distributedReward)
	if err != nil {
		return fmt.Errorf("[oracle] Failed to send coins to distribution module %w", err)
	}

	return nil
}
