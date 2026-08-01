package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

// GetRateSet returns fresh rates for the requested denoms. Every result
// includes the NOAH identity rate and is scoped to the current execution context.
func (k Keeper) GetRateSet(ctx context.Context, denoms ...string) (types.RateSet, error) {
	return k.rateSet(ctx, false, denoms)
}

// GetAvailableRateSet returns fresh rates for whichever requested denoms have
// them, omitting a denom whose rate is unknown or stale instead of failing the
// whole set. Every result includes the NOAH identity rate. Callers that derive
// one value per requested denom need the all-or-nothing GetRateSet; this
// variant serves valuations that price what they can and leave the remainder
// unvalued.
func (k Keeper) GetAvailableRateSet(ctx context.Context, denoms ...string) (types.RateSet, error) {
	return k.rateSet(ctx, true, denoms)
}

func (k Keeper) rateSet(ctx context.Context, skipUnavailable bool, denoms []string) (types.RateSet, error) {
	uniqueDenoms := make([]string, 0, len(denoms))
	seen := map[string]struct{}{chain.NoahBaseDenom: {}}
	for _, denom := range denoms {
		if _, ok := seen[denom]; ok {
			continue
		}
		seen[denom] = struct{}{}
		uniqueDenoms = append(uniqueDenoms, denom)
	}

	rates := types.NewRateSet()
	if len(uniqueDenoms) == 0 {
		return rates, nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()

	for _, denom := range uniqueDenoms {
		rate, err := k.getExchangeRate(ctx, denom, currentTime, params.MaxExchangeRateAge)
		if err != nil {
			if skipUnavailable &&
				(errors.Is(err, types.ErrUnknownDenom) || errors.Is(err, types.ErrStaleExchangeRate)) {
				continue
			}
			return nil, fmt.Errorf("getting exchange rate for denom %s: %w", denom, err)
		}
		rates[denom] = rate
	}

	return rates, nil
}
