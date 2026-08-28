package keeper

import (
	"context"
	"fmt"

	"github.com/ararat-network/ark/x/asset/types"
)

// OraclePricedDenoms returns the sorted denoms the Oracle is the price
// authority for: status ACTIVE or ISSUANCE_HALTED. Every asset is keyed to a
// feed by its own denomination, so nothing else is left to check.
// Consumers derive per-denom membership from this rather than storing their own
// sets.
//
// Feed health deliberately does not enter membership. Membership names the
// authority, not the evidence: freshness is a use-time concern each consumer
// applies against RateSet when it values something, so a stale feed never
// silently changes who is a member and consumers cannot flap with it.
func (k Keeper) OraclePricedDenoms(ctx context.Context) ([]string, error) {
	var denoms []string
	if err := k.Assets.Walk(ctx, nil, func(denom string, asset types.Asset) (bool, error) {
		if asset.IsOraclePriced() {
			denoms = append(denoms, denom)
		}

		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating assets: %w", err)
	}

	return denoms, nil
}

// IsOraclePriced reports whether the Oracle is one registered asset's price
// authority.
func (k Keeper) IsOraclePriced(ctx context.Context, denom string) (bool, error) {
	asset, err := k.GetAsset(ctx, denom)
	if err != nil {
		return false, err
	}
	return asset.IsOraclePriced(), nil
}
