package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"

	"ark/x/oracle/types"
)

// validatorScore caches score weights for reward distribution.
type validatorScore struct {
	addr   sdk.ValAddress
	weight math.Int
}

// SettleRewards distributes oracle rewards by score weight.
func (k Keeper) SettleRewards(ctx context.Context, rewardWindow, rewardDistributionWindow uint64) error {
	// Sum validator score weights.
	votePowerSum := math.ZeroInt()
	validatorScores := []validatorScore{}
	if err := k.ScoreWeight.Walk(ctx, nil, func(validator sdk.ValAddress, scoreWeight math.Int) (bool, error) {
		votePowerSum = votePowerSum.Add(scoreWeight)
		validatorScores = append(validatorScores, validatorScore{
			addr:   validator,
			weight: scoreWeight,
		})
		return false, nil
	}); err != nil {
		return err
	}

	// Skip distribution when no score weights were recorded.
	if votePowerSum.IsZero() {
		k.Logger(ctx).Debug("no votes for this period", "rewardWindow", rewardWindow)
		return nil
	}

	rewardBalances := k.bankKeeper.GetAllBalances(ctx, k.moduleAddress)

	// Skip distribution when the reward pool is empty.
	if rewardBalances.IsZero() {
		k.Logger(ctx).Debug("no rewards for this period", "rewardWindow", rewardWindow)
		return nil
	}

	// periodRewards = oraclePool * rewardWindow / rewardDistributionWindow.
	periodRewards := make(sdk.DecCoins, 0, len(rewardBalances))
	for _, balance := range rewardBalances {
		amount := math.LegacyNewDecFromInt(balance.Amount).
			MulInt(math.NewIntFromUint64(rewardWindow)).
			QuoInt(math.NewIntFromUint64(rewardDistributionWindow))
		periodRewards = append(periodRewards, sdk.NewDecCoinFromDec(balance.Denom, amount))
	}

	// Distribute rewards by score weight.
	var distributedReward sdk.Coins
	rewardEvents := []proto.Message{}
	for _, score := range validatorScores {
		rewardCoins := sdk.NewCoins()
		for _, periodReward := range periodRewards {
			rewardAmt := periodReward.Amount.QuoInt(votePowerSum).MulInt(score.weight).TruncateInt()
			if rewardAmt.IsPositive() {
				rewardCoins = rewardCoins.Add(sdk.NewCoin(periodReward.Denom, rewardAmt))
			}
		}
		if rewardCoins.IsZero() {
			continue
		}

		validator, err := k.stakingKeeper.Validator(ctx, score.addr)
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			k.Logger(ctx).Debug("skipping oracle reward for missing validator", "validator", score.addr.String())
			continue
		}
		if err != nil {
			return fmt.Errorf("getting validator %s for oracle rewards: %w", score.addr, err)
		}
		if validator == nil {
			k.Logger(ctx).Debug("skipping oracle reward for missing validator", "validator", score.addr.String())
			continue
		}

		if err := k.distrKeeper.AllocateTokensToValidator(ctx, validator, sdk.NewDecCoinsFromCoins(rewardCoins...)); err != nil {
			return fmt.Errorf(
				"allocating oracle rewards to %s with reward %s and weight %s: %w",
				score.addr,
				rewardCoins.String(),
				score.weight,
				err,
			)
		}
		distributedReward = distributedReward.Add(rewardCoins...)
		rewardEvents = append(rewardEvents, &types.EventOracleReward{
			Validator: score.addr.String(),
			Rewards:   rewardCoins,
		})
	}

	if distributedReward.IsZero() {
		return nil
	}

	// Move distributed rewards to the distribution module.
	err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, k.distributionName, distributedReward)
	if err != nil {
		return fmt.Errorf("sending coins to distribution module: %w", err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvents(rewardEvents...); err != nil {
		return fmt.Errorf("emitting Oracle reward events: %w", err)
	}

	return nil
}
