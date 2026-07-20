package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"ark/x/oracle/types"
)

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
			bondDenom, err := k.stakingKeeper.BondDenom(ctx)
			if err != nil {
				return true, fmt.Errorf("getting bond denom for oracle slash event: %w", err)
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
