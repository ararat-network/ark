package keeper

import (
	"context"
	"fmt"

	"ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/reserve/types"
)

// FeedReferents implements the oracle module's FeedReferentGuard. The Reserve
// raises two kinds of claim on a feed, both derived from its own state at call
// time: every eligibility entry on the series, and every open position whose
// holding the series prices. Each is reported so governance sees the blocker
// in the removal proposal rather than discovering it later.
func (k Keeper) FeedReferents(ctx context.Context, denom string) ([]oracletypes.FeedReferent, error) {
	var referents []oracletypes.FeedReferent

	// External symbols are found by the series they derive rather than by
	// their own key, because several may share one: `abtc-cb` and `abtc-osl`
	// both pin feed `abtc`. Every entry raises a claim, with no
	// credit-granting test: this guard gates an irreversible-in-practice act,
	// so it fails closed.
	if err := k.RecognitionPolicy.Walk(ctx, nil, func(symbol string, entry types.EligibilityEntry) (bool, error) {
		feed, isExternal := chain.ExternalFeed(symbol)
		if !isExternal || feed != denom {
			return false, nil
		}
		referents = append(referents, oracletypes.FeedReferent{
			Consumer: types.ModuleName,
			Referent: fmt.Sprintf(
				"recognition policy: %s is credited at haircut %s, capped at %s of "+
					"recognised capital, on rates at most %s old",
				symbol,
				entry.HaircutFactor,
				entry.RecognitionCapRatio,
				entry.MaxRateAge,
			),
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating Reserve recognition policy for %s: %w", denom, err)
	}

	// Closed positions raise no claim: their recovery is already crystallised,
	// so no later rate read can change what the ledger says about them.
	external := uint64(0)
	member := uint64(0)
	if err := k.OpenPositions.Walk(ctx, nil, func(_ uint64, position types.Position) (bool, error) {
		// A position pins the series pricing what it holds: the prefix for an
		// external symbol, the denomination itself for Ark paper.
		held := position.Quantity.Denom
		if feed, isExternal := chain.ExternalFeed(held); isExternal {
			if feed == denom {
				external++
			}
			return false, nil
		}
		if held == denom {
			member++
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("counting open %s positions for feed referents: %w", denom, err)
	}
	if external > 0 {
		referents = append(referents, oracletypes.FeedReferent{
			Consumer: types.ModuleName,
			Referent: fmt.Sprintf(
				"%d open position(s) hold external custody priced by %s: with the "+
					"series gone that exposure cannot be listed for recognition, "+
					"which requires an active feed",
				external,
				denom,
			),
		})
	}
	if member > 0 {
		referents = append(referents, oracletypes.FeedReferent{
			Consumer: types.ModuleName,
			Referent: fmt.Sprintf(
				"%d open position(s) hold %s, which is Ark-issued paper: with the "+
					"series gone it cannot be sold out through a deployment, "+
					"leaving a committee burn as its only exit",
				member,
				denom,
			),
		})
	}

	return referents, nil
}

// validateExternalFeeds requires every listed external symbol's series to be
// an active feed: an entry whose series no validator prices is a permanently
// dark row in a fold Treasury settles against every block. Refusing every
// non-Active phase keeps a stored entry's feed Active for as long as it is
// stored, and keeps export and reimport symmetric.
func (k Keeper) validateExternalFeeds(ctx context.Context, entries []types.EligibilityEntry) error {
	for _, entry := range entries {
		feed, isExternal := chain.ExternalFeed(entry.Denom)
		if !isExternal {
			// Unreachable: the caller validates every entry's shape first. Kept
			// because reading a prefix off a name that has none would ask the
			// Oracle about "".
			return fmt.Errorf("eligibility entry %s is not an external symbol", entry.Denom)
		}
		phase, err := k.oracleKeeper.FeedPhase(ctx, feed)
		if err != nil {
			return fmt.Errorf("getting feed phase for %s: %w", feed, err)
		}
		if phase != oracletypes.FeedPhaseActive {
			return fmt.Errorf(
				"external symbol %s prices through feed %s, which is not an active feed",
				entry.Denom,
				feed,
			)
		}
	}
	return nil
}
