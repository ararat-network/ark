package keeper

import (
	"context"
	"fmt"

	"github.com/ararat-network/ark/x/asset/types"
)

// OraclePricedDenoms returns sorted ACTIVE and ISSUANCE_HALTED denominations. Membership identifies
// pricing authority independently of feed health; consumers enforce freshness at use time.
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
