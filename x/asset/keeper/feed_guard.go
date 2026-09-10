package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// FeedReferents claims an asset's own feed while its status is oracle-priced. Suspended,
// written-off, and retired assets do not pin feeds; admission paths require an active feed. Oracle
// guards its own reference denomination.
func (k Keeper) FeedReferents(ctx context.Context, denom string) ([]oracletypes.FeedReferent, error) {
	// The asset registry and the feed registry share one key, so a referent
	// lookup is a point read rather than a scan.
	asset, err := k.Assets.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("getting asset %s for feed referents: %w", denom, err)
	}
	if !asset.IsOraclePriced() {
		return nil, nil
	}

	return []oracletypes.FeedReferent{{
		Consumer: types.ModuleName,
		Referent: fmt.Sprintf("asset %s (%s)", denom, asset.Status),
	}}, nil
}
