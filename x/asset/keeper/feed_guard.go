package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

// FeedReferents implements the oracle module's FeedReferentGuard. x/asset holds
// one claim on a feed, derived from registry state at call time rather than
// indexed. The protocol reference is no longer among them: it is x/oracle's own
// state, and x/oracle checks it directly.
//
// An asset claims its own feed exactly while the Oracle prices it — the claim
// and the price authority are the same fact. Nothing waits on a feed any more:
// every path into the oracle-priced set checks the feed phase
// itself and takes effect in that block, so an asset pins from the moment it is
// registered, SUSPENDED does not pin (RegisterAsset and RecoverAsset re-check
// the phase), and WRITTEN_OFF and RETIRED do not — a tombstone is a status, not
// a claim on price data.
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
