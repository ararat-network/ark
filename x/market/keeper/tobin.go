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

// GetTobinTax resolves a sparse override or the default, including explicit zero. It does not check
// asset eligibility; callers enforce membership and lifecycle.
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

// SetTobinTaxOverride requires registry membership, independent of lifecycle status. Governance and
// committee paths share the write and event rule: unchanged rates emit nothing. Registration and
// override can share a proposal once the feed is active.
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

// RemoveTobinTaxOverride requires an existing override and restores default tracking. It does not
// check lifecycle, so retirement cannot prevent cleanup. Missing entries fail without emitting a
// removal event.
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
