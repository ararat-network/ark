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

	"github.com/ararat-network/ark/x/oracle/types"
)

// RecordVoteAccounting resolves a consensus address and records rewards plus eligible-block
// attendance. Participation counts only when the block is eligible; unresolved validators are
// skipped.
func (k Keeper) RecordVoteAccounting(
	ctx context.Context,
	consAddr sdk.ConsAddress,
	rewardWeight math.Int,
	eligible bool,
	participated bool,
) error {
	if rewardWeight.IsNil() {
		return errors.New("reward weight must be set")
	}
	if rewardWeight.IsNegative() {
		return fmt.Errorf("reward weight must not be negative: %s", rewardWeight)
	}
	if rewardWeight.IsZero() && !eligible {
		return nil
	}

	// Sole cons-addr resolution point. v0.55 key rotation makes this mapping
	// mutable; restore db1fde3's historical-index fallback with the SDK move.
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

	if rewardWeight.IsPositive() {
		currentRewardWeight, err := k.RewardWeight.Get(ctx, valAddr)
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return fmt.Errorf("getting reward weight: %w", err)
			}
			currentRewardWeight = math.ZeroInt()
		}
		if err := k.RewardWeight.Set(ctx, valAddr, currentRewardWeight.Add(rewardWeight)); err != nil {
			return fmt.Errorf("setting reward weight for validator %s: %w", valAddr, err)
		}
	}

	if eligible {
		attendance, err := k.Attendance.Get(ctx, valAddr)
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return fmt.Errorf("getting attendance: %w", err)
		}
		attendance.EligibleBlocks++
		if participated {
			attendance.AttendedBlocks++
		}
		if err := k.Attendance.Set(ctx, valAddr, attendance); err != nil {
			return fmt.Errorf("setting attendance for validator %s: %w", valAddr, err)
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

// SettleAttendance jails, never slashes, validators below the configured attended share of eligible
// blocks. Zero disables jailing. Every record is judged without an extra minimum-block floor; zero
// eligible blocks require zero attendance. See x/oracle/README.md.
func (k Keeper) SettleAttendance(ctx context.Context, attendanceWindowBlocks uint64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// attendanceWindowBlocks is the window carried over in deferred accounting
	// state from when the window opened, so the jail event reports the window its
	// counters actually accumulated under. MinAttendancePerWindow is read live,
	// so a governance change to the ratio applies at this settlement.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	if params.MinAttendancePerWindow.IsZero() {
		return nil
	}

	return k.Attendance.Walk(ctx, nil, func(valAddr sdk.ValAddress, attendance types.Attendance) (bool, error) {
		attended := math.LegacyNewDecFromInt(math.NewIntFromUint64(attendance.AttendedBlocks))
		required := params.MinAttendancePerWindow.MulInt(math.NewIntFromUint64(attendance.EligibleBlocks))
		if attended.GTE(required) {
			return false, nil
		}

		validator, err := k.stakingKeeper.Validator(ctx, valAddr)
		if errors.Is(err, stakingtypes.ErrNoValidatorFound) {
			k.Logger(ctx).Debug("skipping oracle jail for missing validator", "validator", valAddr.String())
			return false, nil
		}
		if err != nil {
			return true, fmt.Errorf("getting validator %s: %w", valAddr, err)
		}
		if validator == nil || validator.IsUnbonded() || validator.IsJailed() {
			return false, nil
		}
		consAddr, err := validator.GetConsAddr()
		if err != nil {
			return true, fmt.Errorf("getting consensus address for validator %s: %w", valAddr, err)
		}
		if err := k.stakingKeeper.Jail(ctx, consAddr); err != nil {
			return true, fmt.Errorf("jailing validator %s for oracle attendance: %w", valAddr, err)
		}
		if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventOracleJail{
			Validator:        valAddr.String(),
			EligibleBlocks:   attendance.EligibleBlocks,
			AttendedBlocks:   attendance.AttendedBlocks,
			AttendanceWindow: attendanceWindowBlocks,
		}); err != nil {
			return true, fmt.Errorf("emitting oracle jail event: %w", err)
		}
		return false, nil
	})
}
