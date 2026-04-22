package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"noah/x/oracle/types"
)

// BuildValidatorScoreMap builds a map of validator scores for all bonded validators in the active set.
func (k Keeper) BuildValidatorScoreMap(ctx context.Context) (map[string]types.ValidatorScore, error) {
	validatorScoreMap := make(map[string]types.ValidatorScore)
	powerReduction := k.stakingKeeper.PowerReduction(ctx)

	if err := k.stakingKeeper.IterateBondedValidatorsByPower(ctx, func(_ int64, validator stakingtypes.ValidatorI) bool {
		operator := validator.GetOperator()
		addrBytes, err := k.stakingKeeper.ValidatorAddressCodec().StringToBytes(operator)
		if err != nil {
			k.Logger(ctx).Warn("failed to decode validator address", "operator", operator, "error", err)
			return false
		}
		validatorScoreMap[operator] = types.NewValidatorScore(
			validator.GetConsensusPower(powerReduction),
			0,
			0,
			sdk.ValAddress(addrBytes),
		)
		return false
	}); err != nil {
		return nil, err
	}

	return validatorScoreMap, nil
}

// CountMisses increments the miss count for validators who failed to vote on all passing denoms.
func (k Keeper) CountMisses(ctx context.Context, voteTargets map[string]math.LegacyDec, validatorScoreMap map[string]types.ValidatorScore) error {
	for _, score := range validatorScoreMap {
		if int(score.WinCount) != len(voteTargets) {
			missCount, err := k.MissCount.Get(ctx, score.Recipient)
			if err != nil && !errors.Is(err, collections.ErrNotFound) {
				return fmt.Errorf("getting miss count: %w", err)
			}
			if err := k.MissCount.Set(ctx, score.Recipient, missCount+1); err != nil {
				return fmt.Errorf("setting miss count: %w", err)
			}
		}
	}

	return nil
}
