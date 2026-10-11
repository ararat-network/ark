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

// GetTobinTaxes resolves each denomination's rate, keyed by denomination: its sparse override,
// including an explicit zero, or the default. It does not check asset eligibility; callers enforce
// membership and lifecycle.
func (k Keeper) GetTobinTaxes(ctx context.Context, denoms ...string) (map[string]math.LegacyDec, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}

	taxes := make(map[string]math.LegacyDec, len(denoms))
	for _, denom := range denoms {
		override, err := k.TobinTaxOverrides.Get(ctx, denom)
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return nil, fmt.Errorf("getting tobin tax override for %s: %w", denom, err)
			}
			override = params.DefaultTobinTax
		}

		taxes[denom] = override
	}

	return taxes, nil
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
