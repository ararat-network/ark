package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/x/oracle/types"
)

// SettleSlash slashes validators who missed too many votes.
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
		// clamp missCount to slashWindow
		if missCount > params.SlashWindow {
			missCount = params.SlashWindow
		}

		// calculate valid vote rate; (slashWindow - missCount) / slashWindow
		validVoteRate := slashWindow.
			Sub(math.LegacyNewDec(int64(missCount))).
			Quo(slashWindow)

		// slash and jail validators who voted less than the minimum required rate
		if validVoteRate.LT(params.MinValidPerWindow) {
			if validator, err := k.stakingKeeper.Validator(ctx, valAddr); validator != nil && validator.IsBonded() && !validator.IsJailed() {
				consAddr, err := validator.GetConsAddr()
				if err != nil {
					k.Logger(ctx).Warn("failed to get consensus address", "validator", validator, "error", err)
					return false, nil
				} else {
					slashAmount, err := k.stakingKeeper.Slash(ctx, consAddr, distributionHeight, validator.GetConsensusPower(powerReduction), params.SlashFraction)
					if err != nil {
						return true, fmt.Errorf("failed to slash validator %s: %w", valAddr, err)
					} else if err := k.stakingKeeper.Jail(ctx, consAddr); err != nil {
						return true, fmt.Errorf("slashed validator, but failed to jail: %w", err)
					}

					sdkCtx.EventManager().EmitEvent(
						sdk.NewEvent(
							types.EventTypeOracleSlash,
							sdk.NewAttribute(types.AttributeKeyValidator, valAddr.String()),
							sdk.NewAttribute(sdk.AttributeKeyAmount, slashAmount.String()),
						),
					)
				}
			} else if err != nil {
				k.Logger(ctx).Warn("failed to get validator", "validator", valAddr, "error", err)
				return false, nil
			}
		}

		return false, nil
	}); err != nil {
		return fmt.Errorf("iterating miss counter: %w", err)
	}

	return nil
}
