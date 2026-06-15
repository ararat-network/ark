package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"noah/x/oracle/types"
)

// SettleSlash slashes validators below the minimum valid vote rate.
func (k Keeper) SettleSlash(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	distributionHeight := height - sdk.ValidatorUpdateDelay - 1

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	powerReduction := k.stakingKeeper.PowerReduction(ctx)
	slashWindow := math.LegacyNewDec(int64(params.SlashWindow))
	if err := k.MissCount.Walk(ctx, nil, func(valAddr sdk.ValAddress, missCount uint64) (bool, error) {
		// Cap missed votes at the slash window.
		if missCount > params.SlashWindow {
			missCount = params.SlashWindow
		}

		// validVoteRate = (slashWindow - missCount) / slashWindow.
		validVoteRate := slashWindow.
			Sub(math.LegacyNewDec(int64(missCount))).
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
			if validator == nil || !validator.IsBonded() || validator.IsJailed() {
				return false, nil
			}
			consAddr, err := validator.GetConsAddr()
			if err != nil {
				return true, fmt.Errorf("getting consensus address for validator %s: %w", valAddr, err)
			}
			slashAmount, err := k.stakingKeeper.Slash(ctx, consAddr, distributionHeight, validator.GetConsensusPower(powerReduction), params.SlashFraction)
			if err != nil {
				return true, fmt.Errorf("failed to slash validator %s: %w", valAddr, err)
			} else if err := k.stakingKeeper.Jail(ctx, consAddr); err != nil {
				return true, fmt.Errorf("slashed validator %s, but failed to jail: %w", valAddr, err)
			}

			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					types.EventTypeOracleSlash,
					sdk.NewAttribute(types.AttributeKeyValidator, valAddr.String()),
					sdk.NewAttribute(sdk.AttributeKeyAmount, slashAmount.String()),
				),
			)
		}

		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating miss counter: %w", err)
	}

	return nil
}
