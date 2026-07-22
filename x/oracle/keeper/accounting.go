package keeper

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"ark/x/oracle/types"
)

// RecordVoteAccounting records reward weight and miss status for the validator
// resolved from a consensus address. If the validator no longer resolves,
// accounting is skipped.
func (k Keeper) RecordVoteAccounting(ctx context.Context, consAddr sdk.ConsAddress, rewardWeight math.Int, missed bool) error {
	if rewardWeight.IsNil() {
		return errors.New("reward weight must be set")
	}
	if rewardWeight.IsNegative() {
		return fmt.Errorf("reward weight must not be negative: %s", rewardWeight)
	}
	if rewardWeight.IsZero() && !missed {
		return nil
	}

	validator, err := k.stakingKeeper.ValidatorByConsAddr(ctx, consAddr)
	if err != nil {
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			return nil
		}
		return fmt.Errorf("getting validator by consensus address %s: %w", consAddr, err)
	}
	if validator == nil {
		return nil
	}

	valAddr, err := sdk.ValAddressFromBech32(validator.GetOperator())
	if err != nil {
		return fmt.Errorf("parsing validator operator address %q: %w", validator.GetOperator(), err)
	}

	updatedRewardWeight := math.ZeroInt()
	if rewardWeight.IsPositive() {
		currentRewardWeight, err := k.RewardWeight.Get(ctx, valAddr)
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return fmt.Errorf("getting reward weight: %w", err)
			}
			currentRewardWeight = math.ZeroInt()
		}
		updatedRewardWeight = currentRewardWeight.Add(rewardWeight)
	}

	var updatedMissCount uint64
	if missed {
		currentMissCount, err := k.MissCount.Get(ctx, valAddr)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting miss count: %w", err)
		}
		updatedMissCount = currentMissCount + 1
	}

	if rewardWeight.IsPositive() {
		if err := k.RewardWeight.Set(ctx, valAddr, updatedRewardWeight); err != nil {
			return fmt.Errorf("setting reward weight for validator %s: %w", validator, err)
		}
	}
	if missed {
		if err := k.MissCount.Set(ctx, valAddr, updatedMissCount); err != nil {
			return fmt.Errorf("setting miss count for validator %s: %w", validator, err)
		}
	}

	return nil
}

// validatorReward pairs a validator with its accumulated reward weight.
type validatorReward struct {
	addr   sdk.ValAddress
	weight math.Int
}

// SettleRewards distributes oracle rewards by reward weight.
func (k Keeper) SettleRewards(ctx context.Context, rewardWindow, rewardDistributionWindow uint64) error {
	// Sum validator reward weights.
	totalRewardWeight := math.ZeroInt()
	validatorRewards := []validatorReward{}
	if err := k.RewardWeight.Walk(ctx, nil, func(validator sdk.ValAddress, rewardWeight math.Int) (bool, error) {
		totalRewardWeight = totalRewardWeight.Add(rewardWeight)
		validatorRewards = append(validatorRewards, validatorReward{
			addr:   validator,
			weight: rewardWeight,
		})
		return false, nil
	}); err != nil {
		return err
	}

	// Skip distribution when no reward weights were recorded.
	if totalRewardWeight.IsZero() {
		k.Logger(ctx).Debug("no votes for this period", "rewardWindow", rewardWindow)
		return nil
	}

	rewardBalances := k.bankKeeper.GetAllBalances(ctx, k.moduleAddress)

	// Skip distribution when the reward pool is empty.
	if rewardBalances.IsZero() {
		k.Logger(ctx).Debug("no rewards for this period", "rewardWindow", rewardWindow)
		return nil
	}

	// rewardsPerWeight = oraclePool * rewardWindow / rewardDistributionWindow / totalRewardWeight.
	rewardsPerWeight := make(sdk.DecCoins, 0, len(rewardBalances))
	for _, balance := range rewardBalances {
		amount := math.LegacyNewDecFromInt(balance.Amount).
			MulInt(math.NewIntFromUint64(rewardWindow)).
			QuoInt(math.NewIntFromUint64(rewardDistributionWindow)).
			QuoInt(totalRewardWeight)
		rewardsPerWeight = append(rewardsPerWeight, sdk.NewDecCoinFromDec(balance.Denom, amount))
	}

	// Distribute rewards by reward weight.
	distributedAmounts := make([]math.Int, len(rewardsPerWeight))
	for i := range distributedAmounts {
		distributedAmounts[i] = math.ZeroInt()
	}
	rewardEvents := make([]proto.Message, 0, len(validatorRewards))
	for _, reward := range validatorRewards {
		rewardCoins := make(sdk.Coins, 0, len(rewardsPerWeight))
		for _, rewardPerWeight := range rewardsPerWeight {
			rewardAmt := rewardPerWeight.Amount.MulInt(reward.weight).TruncateInt()
			if rewardAmt.IsPositive() {
				rewardCoins = append(rewardCoins, sdk.NewCoin(rewardPerWeight.Denom, rewardAmt))
			}
		}
		if rewardCoins.IsZero() {
			continue
		}

		validator, err := k.stakingKeeper.Validator(ctx, reward.addr)
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			k.Logger(ctx).Debug("skipping oracle reward for missing validator", "validator", reward.addr.String())
			continue
		}
		if err != nil {
			return fmt.Errorf("getting validator %s for oracle rewards: %w", reward.addr, err)
		}
		if validator == nil {
			k.Logger(ctx).Debug("skipping oracle reward for missing validator", "validator", reward.addr.String())
			continue
		}

		if err := k.distrKeeper.AllocateTokensToValidator(ctx, validator, sdk.NewDecCoinsFromCoins(rewardCoins...)); err != nil {
			return fmt.Errorf(
				"allocating oracle rewards to %s with reward %s and weight %s: %w",
				reward.addr,
				rewardCoins.String(),
				reward.weight,
				err,
			)
		}
		rewardCoinIndex := 0
		for rewardIndex, rewardPerWeight := range rewardsPerWeight {
			if rewardCoinIndex == len(rewardCoins) {
				break
			}
			rewardCoin := rewardCoins[rewardCoinIndex]
			if rewardCoin.Denom != rewardPerWeight.Denom {
				continue
			}
			distributedAmounts[rewardIndex] = distributedAmounts[rewardIndex].Add(rewardCoin.Amount)
			rewardCoinIndex++
		}
		rewardEvents = append(rewardEvents, &types.EventOracleReward{
			Validator: reward.addr.String(),
			Rewards:   rewardCoins,
		})
	}

	if len(rewardEvents) == 0 {
		return nil
	}
	distributedReward := make(sdk.Coins, 0, len(rewardsPerWeight))
	for rewardIndex, rewardPerWeight := range rewardsPerWeight {
		amount := distributedAmounts[rewardIndex]
		if amount.IsPositive() {
			distributedReward = append(distributedReward, sdk.NewCoin(rewardPerWeight.Denom, amount))
		}
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

// SettleSlash slashes validators below the minimum valid vote rate.
func (k Keeper) SettleSlash(ctx context.Context, slashWindowBlocks uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	distributionHeight := height - sdk.ValidatorUpdateDelay - 1

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	powerReduction := k.stakingKeeper.PowerReduction(ctx)
	bondDenom, err := k.stakingKeeper.BondDenom(ctx)
	if err != nil {
		return fmt.Errorf("getting bond denom for oracle slash event: %w", err)
	}
	slashWindow := math.LegacyNewDecFromInt(math.NewIntFromUint64(slashWindowBlocks))
	if err := k.MissCount.Walk(ctx, nil, func(valAddr sdk.ValAddress, missCount uint64) (bool, error) {
		// Cap missed votes at the slash window.
		if missCount > slashWindowBlocks {
			missCount = slashWindowBlocks
		}

		// validVoteRate = (slashWindow - missCount) / slashWindow.
		validVoteRate := slashWindow.
			Sub(math.LegacyNewDecFromInt(math.NewIntFromUint64(missCount))).
			Quo(slashWindow)

		// Slash and jail validators below the minimum valid vote rate.
		if validVoteRate.LT(params.MinValidPerWindow) {
			validator, err := k.stakingKeeper.Validator(ctx, valAddr)
			if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
				k.Logger(ctx).Debug("skipping oracle slash for missing validator", "validator", valAddr.String())
				return false, nil
			}
			if err != nil {
				return true, fmt.Errorf("getting validator %s: %w", valAddr, err)
			}
			if validator == nil || validator.IsUnbonded() {
				return false, nil
			}
			consAddr, err := validator.GetConsAddr()
			if err != nil {
				return true, fmt.Errorf("getting consensus address for validator %s: %w", valAddr, err)
			}
			consensusPower := sdk.TokensToConsensusPower(validator.GetTokens(), powerReduction)
			slashAmount, err := k.stakingKeeper.Slash(ctx, consAddr, distributionHeight, consensusPower, params.SlashFraction)
			if err != nil {
				return true, fmt.Errorf("failed to slash validator %s: %w", valAddr, err)
			} else if !validator.IsJailed() {
				if err := k.stakingKeeper.Jail(ctx, consAddr); err != nil {
					return true, fmt.Errorf("slashed validator %s, but failed to jail: %w", valAddr, err)
				}
			}
			if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventOracleSlash{
				Validator:   valAddr.String(),
				AmountDenom: bondDenom,
				Amount:      slashAmount,
				MissCount:   missCount,
				SlashWindow: slashWindowBlocks,
			}); err != nil {
				return true, fmt.Errorf("emitting Oracle slash event for %s: %w", valAddr, err)
			}
		}

		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating miss counter: %w", err)
	}

	return nil
}
