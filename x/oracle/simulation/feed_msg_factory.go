package simulation

import (
	"context"
	"slices"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/testutil/simsx"

	"github.com/ararat-network/ark/x/oracle/keeper"
	"github.com/ararat-network/ark/x/oracle/types"
)

// pendingDenoms returns the denominations already carrying a scheduled
// transition. A second transition on one of them conflicts, so neither
// direction may name it.
func pendingDenoms(feeds types.Feeds) []string {
	pending := make([]string, 0, len(feeds.Transitions))
	for _, transition := range feeds.Transitions {
		pending = append(pending, transition.Denom)
	}

	return pending
}

// MsgAddFeedFactory schedules a feed the registry does not yet carry. The
// candidates are the launch denominations: every one is a valid priced
// denomination, and the set is small enough that a run exhausts it and starts
// removing instead.
func MsgAddFeedFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgAddFeed] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgAddFeed) {
		feeds, err := k.Feeds.Get(ctx)
		if err != nil {
			reporter.Skip("get feeds: " + err.Error())

			return nil, nil
		}
		pending := pendingDenoms(feeds)

		candidates := make([]string, 0, len(types.DefaultFeedDenoms))
		for _, denom := range types.DefaultFeedDenoms {
			if slices.Contains(feeds.Denoms, denom) || slices.Contains(pending, denom) {
				continue
			}
			candidates = append(candidates, denom)
		}
		if len(candidates) == 0 {
			reporter.Skip("every launch denomination already has a feed or a pending transition")

			return nil, nil
		}

		return nil, &types.MsgAddFeed{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Denom:     candidates[testData.Rand().Intn(len(candidates))],
		}
	}
}

// MsgRemoveFeedFactory schedules the removal of a feed nothing refers to. A
// referenced feed is refused, and most active feeds are referenced — an asset
// prices through one, or the protocol reference names it — so the unreferenced
// remainder is what governance could actually retire.
func MsgRemoveFeedFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgRemoveFeed] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgRemoveFeed) {
		feeds, err := k.Feeds.Get(ctx)
		if err != nil {
			reporter.Skip("get feeds: " + err.Error())

			return nil, nil
		}
		pending := pendingDenoms(feeds)

		candidates := make([]string, 0, len(feeds.Denoms))
		for _, denom := range feeds.Denoms {
			if slices.Contains(pending, denom) {
				continue
			}
			referents, err := k.FeedReferents(ctx, denom)
			if err != nil {
				reporter.Skip("get feed referents: " + err.Error())

				return nil, nil
			}
			if len(referents) == 0 {
				candidates = append(candidates, denom)
			}
		}
		if len(candidates) == 0 {
			reporter.Skip("every feed is referenced or already transitioning")

			return nil, nil
		}

		return nil, &types.MsgRemoveFeed{
			Authority: testData.ModuleAccountAddress(reporter, "gov"),
			Denom:     candidates[testData.Rand().Intn(len(candidates))],
		}
	}
}

// MsgSetReferenceDenomFactory re-points the protocol reference at an active
// feed. Re-pointing rebases every stored rate through the outgoing rate, so
// the rate stays positive and inside the bound the handler states.
func MsgSetReferenceDenomFactory(k *keeper.Keeper) simsx.SimMsgFactoryFn[*types.MsgSetReferenceDenom] {
	return func(
		ctx context.Context,
		testData *simsx.ChainDataSource,
		reporter simsx.SimulationReporter,
	) ([]simsx.SimAccount, *types.MsgSetReferenceDenom) {
		feeds, err := k.Feeds.Get(ctx)
		if err != nil {
			reporter.Skip("get feeds: " + err.Error())

			return nil, nil
		}
		if len(feeds.Denoms) == 0 {
			reporter.Skip("no active feed to reference")

			return nil, nil
		}

		r := testData.Rand()

		return nil, &types.MsgSetReferenceDenom{
			Authority:      testData.ModuleAccountAddress(reporter, "gov"),
			ReferenceDenom: feeds.Denoms[r.Intn(len(feeds.Denoms))],
			OutgoingRate:   math.LegacyNewDecWithPrec(int64(r.IntInRange(1, 1_000_000)), 6),
		}
	}
}
