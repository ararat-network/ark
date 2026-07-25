package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

// GetRateSnapshot returns fresh rates for the requested denoms. Every result
// includes the NOAH identity rate and is scoped to the current execution context.
func (k Keeper) GetRateSnapshot(ctx context.Context, denoms ...string) (types.RateSnapshot, error) {
	uniqueDenoms := make([]string, 0, len(denoms))
	seen := map[string]struct{}{chain.NoahBaseDenom: {}}
	for _, denom := range denoms {
		if _, ok := seen[denom]; ok {
			continue
		}
		seen[denom] = struct{}{}
		uniqueDenoms = append(uniqueDenoms, denom)
	}

	rates := make(types.RateSnapshot, len(uniqueDenoms)+1)
	rates[chain.NoahBaseDenom] = math.LegacyOneDec()
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
			return nil, fmt.Errorf("getting exchange rate for denom %s: %w", denom, err)
		}
		rates[denom] = rate
	}

	return rates, nil
}
