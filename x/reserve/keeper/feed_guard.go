package keeper

import (
	"context"
	"fmt"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

// FeedReferents derives blockers from eligibility entries and open positions using each feed. It
// reports every claim so governance can resolve dependencies before scheduling removal.
func (k Keeper) FeedReferents(ctx context.Context, denom string) ([]oracletypes.FeedReferent, error) {
	var referents []oracletypes.FeedReferent

	// Match external symbols by their underlying series: multiple claims can pin one feed. Every
	// eligibility entry blocks removal, even if it currently earns no credit.
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

// validateExternalFeeds requires active feeds for all listed external symbols. Referent guards keep
// those feeds active while entries remain, preserving export/import validity.
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
