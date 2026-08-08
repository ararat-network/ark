package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

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

// GetLastKnownRateSet returns the most recently stored rate for whichever
// requested denoms have ever been priced, ignoring the freshness window that
// GetRateSet and GetAvailableRateSet enforce. A denom the Oracle has never
// priced is omitted, exactly as it is from the available set.
//
// This is for sizing an aggregate against supply already outstanding, never for
// quoting a trade or minting against. The distinction is what the rate is used
// to decide: a stale rate cannot say what one unit is worth to a counterparty
// now, but it remains the best evidence of what a standing obligation is worth
// relative to the others it shares a fund with, and dropping that supply from
// such a total silently redistributes the fund toward whoever is transacting.
// Every caller must be able to state which of the two it is doing.
func (k Keeper) GetLastKnownRateSet(ctx context.Context, denoms ...string) (types.RateSet, error) {
	rates := types.NewRateSet()
	seen := map[string]struct{}{chain.NoahBaseDenom: {}}
	for _, denom := range denoms {
		if _, ok := seen[denom]; ok {
			continue
		}
		seen[denom] = struct{}{}

		// The store read is deliberately direct rather than through
		// getExchangeRate: bypassing the age gate is the whole purpose, and
		// routing through the checked helper only to discard its verdict would
		// invite someone to "fix" the duplication by reinstating the gate.
		stored, err := k.ExchangeRate.Get(ctx, denom)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("getting last known exchange rate for denom %s: %w", denom, err)
		}
		if stored.Rate.IsNil() || !stored.Rate.IsPositive() {
			continue
		}
		rates[denom] = stored.Rate
	}

	return rates, nil
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
		maxAge, err := k.getMaxAge(ctx, params, denom)
		if err != nil {
			return nil, err
		}

		rate, err := k.getExchangeRate(ctx, denom, currentTime, maxAge)
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
