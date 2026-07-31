package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"ark/x/market/types"
)

// GetTobinTax returns the effective rate a conversion leg in this denomination
// contributes: its override when governance set one, the default otherwise.
//
// Rates were never derivable — they are governance judgment about how much
// oracle error a market can accumulate between updates — so a newly activated
// asset converts at the default from its first live block with no policy act
// required. Eligibility is the caller's concern: a rate exists for any
// denomination, including one no asset is listed under.
func (k Keeper) GetTobinTax(ctx context.Context, denom string) (math.LegacyDec, error) {
	override, err := k.TobinTaxOverrides.Get(ctx, denom)
	if err == nil {
		return override, nil
	}
	if !errors.Is(err, collections.ErrNotFound) {
		return math.LegacyDec{}, fmt.Errorf("getting tobin tax override for %s: %w", denom, err)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("getting params: %w", err)
	}

	return params.DefaultTobinTax, nil
}

// GetTobinTaxOverrides returns the sparse overrides in key order, which is the
// order genesis export and the query both need.
func (k Keeper) GetTobinTaxOverrides(ctx context.Context) ([]types.TobinTaxOverride, error) {
	overrides := []types.TobinTaxOverride{}
	if err := k.TobinTaxOverrides.Walk(
		ctx,
		nil,
		func(denom string, tobinTax math.LegacyDec) (bool, error) {
			overrides = append(overrides, types.TobinTaxOverride{
				Denom:    denom,
				TobinTax: tobinTax,
			})

			return false, nil
		},
	); err != nil {
		return nil, fmt.Errorf("iterating tobin tax overrides: %w", err)
	}

	return overrides, nil
}

// SetTobinTaxOverride records one per-denomination exception.
//
// The denomination must identify a registered asset, and PENDING counts: a
// listing proposal can add the feed, register the asset, and set the override
// together, so an illiquid listing never spends a block live at a default rate
// chosen for liquid fiat. The check exists because a dangling override fails
// silently — the protection governance wrote would simply not apply to the
// denomination it meant — and a loud proposal failure is the only way that
// mistake surfaces.
func (k Keeper) SetTobinTaxOverride(ctx context.Context, denom string, tobinTax math.LegacyDec) error {
	if err := types.ValidateTobinTax(tobinTax); err != nil {
		return err
	}
	if _, err := k.assetKeeper.GetAsset(ctx, denom); err != nil {
		return err
	}
	if err := k.TobinTaxOverrides.Set(ctx, denom, tobinTax); err != nil {
		return fmt.Errorf("setting tobin tax override for %s: %w", denom, err)
	}

	return nil
}

// RemoveTobinTaxOverride deletes one per-denomination exception, returning that
// denomination to the default.
//
// The entry must exist, so a typo fails rather than reporting success on a
// denomination that was never overridden. The asset registry is deliberately
// not consulted: retirement leaves overrides in place — a cleanup hook would
// make asset lifecycle write Market state, the coupling the asset-lock index
// was deleted to avoid — so removing an ex-member's entry must stay possible.
func (k Keeper) RemoveTobinTaxOverride(ctx context.Context, denom string) error {
	found, err := k.TobinTaxOverrides.Has(ctx, denom)
	if err != nil {
		return fmt.Errorf("checking tobin tax override for %s: %w", denom, err)
	}
	if !found {
		return types.ErrTobinOverrideMissing.Wrap(denom)
	}
	if err := k.TobinTaxOverrides.Remove(ctx, denom); err != nil {
		return fmt.Errorf("removing tobin tax override for %s: %w", denom, err)
	}

	return nil
}
