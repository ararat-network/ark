package keeper

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/types"
)

// GetFeeds returns the feed epoch validators must report for voteHeight.
func (k Keeper) GetFeeds(ctx context.Context, voteHeight int64) (types.FeedSet, error) {
	feeds, err := k.Feeds.Get(ctx)
	if err != nil {
		return types.FeedSet{}, fmt.Errorf("getting feeds: %w", err)
	}

	return feeds.AtHeight(voteHeight), nil
}

// FeedPhase returns denom's relationship to the active feed set and its own
// scheduled transition, if it has one.
func (k Keeper) FeedPhase(ctx context.Context, denom string) (types.FeedPhase, error) {
	feeds, err := k.Feeds.Get(ctx)
	if err != nil {
		return types.FeedPhaseOff, fmt.Errorf("getting feeds: %w", err)
	}

	return feeds.Phase(denom), nil
}

// ScheduleFeedTransition stages one feed's membership change. Contention is per
// feed: a transition in flight for another feed is irrelevant here.
func (k Keeper) ScheduleFeedTransition(ctx context.Context, denom string, direction types.FeedDirection) error {
	if err := chain.ValidatePricedDenom(denom); err != nil {
		return err
	}
	feeds, err := k.Feeds.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting feeds: %w", err)
	}
	for _, scheduled := range feeds.Transitions {
		if scheduled.Denom != denom {
			continue
		}
		if scheduled.Direction == direction {
			return nil
		}

		// A record is immutable once written, so the opposite direction has to
		// wait for activation rather than amending or cancelling this one.
		return sdkerrors.Wrapf(
			types.ErrFeedTransitionPending,
			"feed %s has a conflicting transition activating at vote height %d",
			denom,
			scheduled.ActivationVoteHeight,
		)
	}

	_, active := slices.BinarySearch(feeds.Denoms, denom)
	if direction == types.FeedDirection_FEED_DIRECTION_ADD && active ||
		direction == types.FeedDirection_FEED_DIRECTION_REMOVE && !active {
		return nil
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	transition := types.FeedTransition{
		Denom:                denom,
		Direction:            direction,
		ActivationVoteHeight: sdkCtx.BlockHeight() + types.FeedActivationDelayBlocks,
	}
	scheduled := feeds
	scheduled.Transitions = append(slices.Clone(feeds.Transitions), transition)
	slices.SortFunc(scheduled.Transitions, func(a, b types.FeedTransition) int {
		if a.ActivationVoteHeight != b.ActivationVoteHeight {
			return cmp.Compare(a.ActivationVoteHeight, b.ActivationVoteHeight)
		}

		return cmp.Compare(a.Denom, b.Denom)
	})
	if err := scheduled.Validate(); err != nil {
		return fmt.Errorf("validating scheduled feed transition: %w", err)
	}

	if err := k.Feeds.Set(ctx, scheduled); err != nil {
		return fmt.Errorf("scheduling feed transition: %w", err)
	}

	if err := sdkCtx.EventManager().EmitTypedEvent(
		&types.EventFeedTransitionScheduled{
			Denom:                transition.Denom,
			Direction:            transition.Direction,
			ActivationVoteHeight: transition.ActivationVoteHeight,
			ResultingVersion: scheduled.
				AtHeight(transition.ActivationVoteHeight).
				Version,
		},
	); err != nil {
		return fmt.Errorf("emitting scheduled feed transition: %w", err)
	}

	return nil
}

// AdvanceFeeds promotes every due transition batch after the previous vote
// height has been consumed. It must run after vote extensions are processed:
// the tally for vote height V reads the fold at V, and promoting a batch due at
// V before that read would validate votes against a set no validator could have
// seen. Rates for removed feeds are pruned here; the oracle owns both the feed
// registry and rate storage.
func (k Keeper) AdvanceFeeds(ctx context.Context) error {
	feeds, err := k.Feeds.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting feeds: %w", err)
	}
	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()

	// Each pass promotes the leading run of transitions sharing the earliest
	// activation height, once that height has arrived.
	for len(feeds.Transitions) > 0 && feeds.Transitions[0].ActivationVoteHeight <= blockHeight {
		batchHeight := feeds.Transitions[0].ActivationVoteHeight
		size := 0
		for _, transition := range feeds.Transitions {
			if transition.ActivationVoteHeight != batchHeight {
				break
			}
			size++
		}

		promoted, err := k.activateFeedBatch(ctx, feeds, feeds.Transitions[:size])
		if err != nil {
			return err
		}
		feeds = promoted
	}

	return nil
}

// activateFeedBatch applies one activation batch: it advances the feed set by
// one version, prunes rates for removed feeds, and emits the batch event.
func (k Keeper) activateFeedBatch(ctx context.Context, feeds types.Feeds, batch []types.FeedTransition) (types.Feeds, error) {
	promoted := types.Feeds{
		Denoms:      slices.Clone(feeds.Denoms),
		Version:     feeds.Version + 1,
		Transitions: slices.Clone(feeds.Transitions[len(batch):]),
	}
	added := make([]string, 0, len(batch))
	removed := make([]string, 0, len(batch))
	for _, transition := range batch {
		promoted.ApplyTransition(transition)
		if transition.Direction == types.FeedDirection_FEED_DIRECTION_ADD {
			added = append(added, transition.Denom)
			continue
		}
		removed = append(removed, transition.Denom)
	}
	// Every remaining record is for a feed this batch did not touch, because at
	// most one record exists per feed, so the direction invariants still hold
	// against the promoted set.
	if err := promoted.Validate(); err != nil {
		return types.Feeds{}, fmt.Errorf("validating promoted feeds: %w", err)
	}

	// A rate that survived removal would be stale on the feed's return, so
	// removal prunes it and a re-added feed re-warms.
	for _, denom := range removed {
		if err := k.ExchangeRate.Remove(ctx, denom); err != nil {
			return types.Feeds{}, fmt.Errorf(
				"removing exchange rate for feed %s: %w",
				denom,
				err,
			)
		}
	}
	if err := k.Feeds.Set(ctx, promoted); err != nil {
		return types.Feeds{}, fmt.Errorf("advancing feeds: %w", err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventFeedsActivated{
			Version:       promoted.Version,
			AddedDenoms:   added,
			RemovedDenoms: removed,
		},
	); err != nil {
		return types.Feeds{}, fmt.Errorf("emitting activated feeds: %w", err)
	}

	return promoted, nil
}
