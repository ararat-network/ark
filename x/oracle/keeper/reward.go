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

// RewardBallotWinners will give out portion of seigniorage reward(reward-weight) to the
// oracle voters that voted faithfully at the end of every VotePeriod.
func (k Keeper) RewardBallotWinners(
	ctx context.Context,
	votePeriod int64,
	rewardDistributionWindow int64,
	ballotWinners map[string]types.ValidatorScore,
) error {
	// Sum weight of the claims
	ballotPowerSum := int64(0)
	for _, winner := range ballotWinners {
		ballotPowerSum += winner.Weight
	}

	// Exit if the ballot is empty
	if ballotPowerSum == 0 {
		return errors.New("empty ballot")
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

	// Dole out rewards
	var distributedReward sdk.Coins
	for _, winner := range ballotWinners {
		rewardCoins := sdk.NewCoins()
		receiverVal := k.stakingKeeper.Validator(ctx, winner.Recipient)

		// Reflects contribution
		rewardAmt := periodRewards.QuoInt64(ballotPowerSum).MulInt64(winner.Weight).TruncateInt()
		rewardCoins = append(rewardCoins, sdk.NewCoin(core.MicroArkDenom, rewardAmt))

		// In case absence of the validator, we just skip distribution
		if receiverVal != nil && !rewardCoins.IsZero() {
			k.distrKeeper.AllocateTokensToValidator(ctx, receiverVal, sdk.NewDecCoinsFromCoins(rewardCoins...))
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
