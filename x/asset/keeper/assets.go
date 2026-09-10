package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/asset/types"
)

// GetAsset returns one registered asset, or ErrAssetNotFound. Consumers reach
// the registry through this rather than the collection because the domain error
// is what they branch on, and because an interface cannot carry a collection.
func (k Keeper) GetAsset(ctx context.Context, denom string) (types.Asset, error) {
	asset, err := k.Assets.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Asset{}, errorsmod.Wrap(types.ErrAssetNotFound, denom)
		}
		return types.Asset{}, fmt.Errorf("getting asset %s: %w", denom, err)
	}

	return asset, nil
}

// HasAsset reports permanent registry membership, including written-off and retired assets.
// Membership excludes Ark-issued denominations from capital credit; callers needing lifecycle
// status must read the record.
func (k Keeper) HasAsset(ctx context.Context, denom string) (bool, error) {
	registered, err := k.Assets.Has(ctx, denom)
	if err != nil {
		return false, fmt.Errorf("checking asset %s: %w", denom, err)
	}

	return registered, nil
}

// ListAssets returns every registered asset in key order. The lifecycle queries
// are folds over it; nothing stores a membership set of its own.
func (k Keeper) ListAssets(ctx context.Context) ([]types.Asset, error) {
	assets := []types.Asset{}
	if err := k.Assets.Walk(ctx, nil, func(_ string, asset types.Asset) (bool, error) {
		assets = append(assets, asset)

		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating assets: %w", err)
	}

	return assets, nil
}

func (k Keeper) getAssetAtVersion(ctx context.Context, denom string, expectedVersion uint64) (types.Asset, error) {
	asset, err := k.GetAsset(ctx, denom)
	if err != nil {
		return types.Asset{}, err
	}
	if asset.Version != expectedVersion {
		return types.Asset{}, errorsmod.Wrapf(
			types.ErrAssetVersionMismatch,
			"asset %s has version %d, expected %d",
			denom,
			asset.Version,
			expectedVersion,
		)
	}

	return asset, nil
}

// advanceAsset stores the caller's governance mutation, increments its lifecycle version, and emits
// a status event even when only the version changes.
func (k Keeper) advanceAsset(ctx context.Context, before types.Asset, after types.Asset) error {
	after.Version = before.Version + 1
	if err := k.Assets.Set(ctx, after.Denom, after); err != nil {
		return fmt.Errorf("advancing asset %s: %w", after.Denom, err)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventAssetStatusChanged{
			Denom:     after.Denom,
			OldStatus: before.Status,
			NewStatus: after.Status,
			Version:   after.Version,
		},
	); err != nil {
		return fmt.Errorf("emitting status for asset %s: %w", after.Denom, err)
	}

	return nil
}

// AssetSupply returns the outstanding supply of a denomination.
func (k Keeper) AssetSupply(ctx context.Context, denom string) sdk.Coin {
	return k.bankKeeper.GetSupply(ctx, denom)
}
