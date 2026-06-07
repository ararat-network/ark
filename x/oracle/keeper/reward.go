package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/types"
)

// validatorScore is used to store the scores from the store locally for reward calculations
type validatorScore struct {
	addr   sdk.ValAddress
	weight uint64
}

// SettleRewards will give out a portion of seigniorage reward (rewardWeight) to the
// oracle voters that voted faithfully at the end of every VotePeriod.
func (k Keeper) SettleRewards(ctx context.Context, rewardWindow, rewardDistributionWindow uint64) error {
	// sum weight of the scores
	votePowerSum := math.ZeroInt()
	validatorScores := []validatorScore{}
	if err := k.ScoreWeight.Walk(ctx, nil, func(validator sdk.ValAddress, scoreWeight uint64) (bool, error) {
		votePowerSum = votePowerSum.Add(math.NewIntFromUint64(scoreWeight))
		validatorScores = append(validatorScores, validatorScore{
			addr:   validator,
			weight: scoreWeight,
		})
		return false, nil
	}); err != nil {
		return err
	}

	// return if there are no votes
	if votePowerSum.IsZero() {
		k.Logger(ctx).Info("no votes for this period", "rewardWindow", rewardWindow)
		return nil
	}

	rewardAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	rewardPool := k.bankKeeper.GetAllBalances(ctx, rewardAcc.GetAddress())

	// return if there's no rewards to give out
	if rewardPool.IsZero() {
		k.Logger(ctx).Info("no rewards for this period", "rewardWindow", rewardWindow)
		return nil
	}

	// rewardCoin  = oraclePool * VotePeriod / RewardDistributionWindow
	periodRewards := math.LegacyNewDecFromInt(rewardPool.AmountOf(core.MicroArkDenom)).
		MulInt64(int64(rewardWindow)).
		QuoInt64(int64(rewardDistributionWindow))

	// distribute rewards
	var distributedReward sdk.Coins
	rewardEvents := sdk.Events{}
	for _, score := range validatorScores {
		rewardAmt := periodRewards.QuoInt(votePowerSum).MulInt64(int64(score.weight)).TruncateInt()
		rewardCoins := sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, rewardAmt))
		if rewardCoins.IsZero() {
			continue
		}

		validator, err := k.stakingKeeper.Validator(ctx, score.addr)
		if err != nil {
			return fmt.Errorf("getting validator %s for oracle rewards: %w", score.addr, err)
		}
		if validator == nil {
			return fmt.Errorf("validator not found for oracle rewards: %s", score.addr)
		}

		if err := k.distrKeeper.AllocateTokensToValidator(ctx, validator, sdk.NewDecCoinsFromCoins(rewardCoins...)); err != nil {
			return fmt.Errorf(
				"allocating oracle rewards to %s with reward %s and weight %d: %w",
				score.addr,
				rewardCoins.String(),
				score.weight,
				err,
			)
		}
		distributedReward = distributedReward.Add(rewardCoins...)
		rewardEvents = append(rewardEvents, sdk.NewEvent(
			types.EventTypeOracleReward,
			sdk.NewAttribute(types.AttributeKeyValidator, score.addr.String()),
			sdk.NewAttribute(types.AttributeKeyRewardAmount, rewardCoins.String()),
		))
	}

	// Move distributed reward to distribution module
	err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.distributionName, distributedReward)
	if err != nil {
		return fmt.Errorf("sending coins to distribution module: %w", err)
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvents(rewardEvents)

	return nil
}
