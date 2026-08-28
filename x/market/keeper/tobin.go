package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/market/types"
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
// The denomination must identify a registered asset, and status is not
// consulted beyond that: a listing proposal can add the feed, register the
// asset, and set the override together, so an illiquid listing never spends a
// block live at a default rate chosen for liquid fiat. The check exists because
// a dangling override fails silently — the protection governance wrote would
// simply not apply to the denomination it meant — and a loud proposal failure
// is the only way that mistake surfaces.
//
// A rate matching what is already stored writes nothing and announces nothing,
// so EventTobinTaxOverrideSet means a rate moved rather than that a proposal
// ran. Both writers reach this method, so governance and the committee share
// one rule and one event.
func (k Keeper) SetTobinTaxOverride(ctx context.Context, denom string, tobinTax math.LegacyDec) error {
	if err := types.ValidateTobinTax(tobinTax); err != nil {
		return err
	}
	if _, err := k.assetKeeper.GetAsset(ctx, denom); err != nil {
		return err
	}

	// Equal, not ==: a LegacyDec wraps a *big.Int, so == compares pointers and
	// would call every restatement a change.
	stored, err := k.TobinTaxOverrides.Get(ctx, denom)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("getting tobin tax override for %s: %w", denom, err)
	}
	if err == nil && stored.Equal(tobinTax) {
		return nil
	}

	if err := k.TobinTaxOverrides.Set(ctx, denom, tobinTax); err != nil {
		return fmt.Errorf("setting tobin tax override for %s: %w", denom, err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTobinTaxOverrideSet{
		Denom:    denom,
		TobinTax: tobinTax,
	}); err != nil {
		return fmt.Errorf("emitting Market tobin tax override: %w", err)
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
//
// The existence check is what keeps the event honest here: a removal that found
// nothing errors rather than announcing a deletion that did not happen, so this
// path needs no equivalent of the set path's no-op guard.
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

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTobinTaxOverrideRemoved{
		Denom: denom,
	}); err != nil {
		return fmt.Errorf("emitting Market tobin tax override removal: %w", err)
	}

	return nil
}
