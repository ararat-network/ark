package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/types"
)

// GetRateSet returns fresh rates for the requested denoms. Every result
// includes the NOAH identity rate and is scoped to the current execution context.
func (k Keeper) GetRateSet(ctx context.Context, denoms ...string) (types.RateSet, error) {
	return k.rateSet(ctx, false, denoms)
}

// GetAvailableRateSet returns fresh requested rates plus NOAH identity, omitting unknown or stale
// feeds. Use GetRateSet when every requested denomination must be priced.
func (k Keeper) GetAvailableRateSet(ctx context.Context, denoms ...string) (types.RateSet, error) {
	return k.rateSet(ctx, true, denoms)
}

// GetLastKnownRateSet ignores freshness but omits never-priced denominations. It supports
// accounting for outstanding supply only, never quotes, minting, or payments. Every set includes
// NOAH identity.
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

// GetRateSetWithin applies each request's window to its derived feed and keys results by requested
// denomination. Unknown, non-positive, stale, or non-positive-window entries are omitted; NOAH
// identity remains. See x/oracle/README.md for consumer freshness policy.
func (k Keeper) GetRateSetWithin(ctx context.Context, requests []types.RateRequest) (types.RateSet, error) {
	rates := types.NewRateSet()
	currentTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		if _, duplicate := seen[request.Denom]; duplicate {
			return nil, fmt.Errorf("duplicate rate request for %s", request.Denom)
		}
		seen[request.Denom] = struct{}{}
		if request.MaxAge <= 0 {
			continue
		}
		feed := request.Denom
		if derived, isExternal := chain.ExternalFeed(request.Denom); isExternal {
			feed = derived
		}

		// Read directly rather than through getExchangeRate, which enforces
		// the default window: the request's window governs here, and routing
		// through the checked helper only to discard its verdict would invite
		// someone to "fix" the duplication by reinstating it.
		stored, err := k.ExchangeRate.Get(ctx, feed)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("getting exchange rate for feed %s: %w", feed, err)
		}
		if currentTime.Sub(stored.BlockTimestamp) > request.MaxAge {
			continue
		}
		rates[request.Denom] = stored.Rate
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
