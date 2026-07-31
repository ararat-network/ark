package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

// Feeds are keyed by the denomination they price, so these read as denoms
// even where no asset is listed under them.
const (
	feedGold   = "agold"
	feedSilver = "asilver"
	feedUSD    = "ausd"
)

// seedFeeds installs an active feed set at the initial version with no
// transitions in flight.
func (s *KeeperTestSuite) seedFeeds(denoms ...string) {
	s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds(denoms)))
}

func (s *KeeperTestSuite) scheduleAdd(denom string) error {
	return s.keeper.ScheduleFeedTransition(
		s.ctx,
		denom,
		types.FeedDirection_FEED_DIRECTION_ADD,
	)
}

func (s *KeeperTestSuite) scheduleRemove(denom string) error {
	return s.keeper.ScheduleFeedTransition(
		s.ctx,
		denom,
		types.FeedDirection_FEED_DIRECTION_REMOVE,
	)
}

func (s *KeeperTestSuite) TestGetFeedsFoldsAtHeight() {
	s.seedFeeds(feedUSD)
	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.FeedActivationDelayBlocks
	s.Require().NoError(s.scheduleAdd(feedGold))

	before, err := s.keeper.GetFeeds(s.ctx, activationHeight-1)
	s.Require().NoError(err)
	s.Require().Equal(types.InitialFeedVersion, before.Version)
	s.Require().Equal([]string{feedUSD}, before.Denoms)

	at, err := s.keeper.GetFeeds(s.ctx, activationHeight)
	s.Require().NoError(err)
	s.Require().Equal(types.InitialFeedVersion+1, at.Version)
	s.Require().Equal([]string{feedGold, feedUSD}, at.Denoms)
}

func (s *KeeperTestSuite) TestFeedPhaseReadsRegistry() {
	testCases := []struct {
		name  string
		denom string
		phase types.FeedPhase
	}{
		{name: "active", denom: feedUSD, phase: types.FeedPhaseActive},
		{name: "adding", denom: feedSilver, phase: types.FeedPhaseAdding},
		{name: "removing", denom: feedGold, phase: types.FeedPhaseRemoving},
		{name: "off", denom: "azinc", phase: types.FeedPhaseOff},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Each sub-test runs against fresh suite state, so the registry is
			// seeded inside the closure.
			s.seedFeeds(feedUSD, feedGold)
			s.Require().NoError(s.scheduleAdd(feedSilver))
			s.Require().NoError(s.scheduleRemove(feedGold))

			phase, err := s.keeper.FeedPhase(s.ctx, tc.denom)
			s.Require().NoError(err)
			s.Require().Equal(tc.phase, phase)
		})
	}
}

func (s *KeeperTestSuite) TestScheduleFeedTransitionRejectsInvalidDenom() {
	s.seedFeeds(feedUSD)

	s.Require().ErrorContains(
		s.scheduleAdd("aGOLD"),
		"must be an Ark-native base denom matching",
	)
	s.Require().ErrorContains(s.scheduleAdd("anoah"), "is the numeraire and is never priced")
}

func (s *KeeperTestSuite) TestScheduleFeedTransitionIsIdempotentPerDirection() {
	s.seedFeeds(feedUSD)

	s.Require().NoError(s.scheduleAdd(feedGold))
	s.Require().NoError(s.scheduleAdd(feedGold))

	feeds, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(feeds.Transitions, 1)
	s.Require().Equal(feedGold, feeds.Transitions[0].Denom)
}

func (s *KeeperTestSuite) TestScheduleFeedTransitionRejectsOppositeDirection() {
	s.seedFeeds(feedUSD)
	s.Require().NoError(s.scheduleAdd(feedGold))

	s.Require().ErrorIs(s.scheduleRemove(feedGold), types.ErrFeedTransitionPending)

	feeds, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(feeds.Transitions, 1)
	s.Require().Equal(
		types.FeedDirection_FEED_DIRECTION_ADD,
		feeds.Transitions[0].Direction,
	)
}

func (s *KeeperTestSuite) TestScheduleFeedTransitionNoOpsWhenAlreadySettled() {
	s.seedFeeds(feedUSD)

	s.Require().NoError(s.scheduleAdd(feedUSD))
	s.Require().NoError(s.scheduleRemove(feedGold))

	feeds, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(feeds.Transitions)
}

func (s *KeeperTestSuite) TestScheduleFeedTransitionsInOneBlockShareOneBatch() {
	s.seedFeeds(feedUSD)
	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.FeedActivationDelayBlocks

	s.Require().NoError(s.scheduleAdd(feedGold))
	s.Require().NoError(s.scheduleAdd(feedSilver))

	feeds, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(feeds.Transitions, 2)
	for _, transition := range feeds.Transitions {
		s.Require().Equal(activationHeight, transition.ActivationVoteHeight)
	}

	// One batch is one version bump, however many feeds it carries.
	activationCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(activationHeight)
	s.Require().NoError(s.keeper.AdvanceFeeds(activationCtx))

	promoted, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(promoted.Transitions)
	s.Require().Equal(types.InitialFeedVersion+1, promoted.Version)
	s.Require().Equal([]string{feedGold, feedSilver, feedUSD}, promoted.Denoms)
}

// The scheduled event reports the version in effect once the feed goes live,
// which is past every batch already due to activate before it. Reporting one
// past the current version would understate it whenever another feed has a
// transition in flight at an earlier height.
func (s *KeeperTestSuite) TestScheduleFeedTransitionCountsPendingBatchesInResultingVersion() {
	s.seedFeeds(feedUSD)
	baseHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight()

	s.Require().NoError(s.scheduleAdd(feedGold))

	// A later block schedules a second batch, which cannot activate until the
	// first has already advanced the version.
	nextCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(baseHeight + 1)
	s.Require().NoError(s.keeper.ScheduleFeedTransition(
		nextCtx,
		feedSilver,
		types.FeedDirection_FEED_DIRECTION_ADD,
	))

	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventFeedTransitionScheduled{
			Denom:                feedGold,
			Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
			ActivationVoteHeight: baseHeight + types.FeedActivationDelayBlocks,
			ResultingVersion:     types.InitialFeedVersion + 1,
		},
		&types.EventFeedTransitionScheduled{
			Denom:                feedSilver,
			Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
			ActivationVoteHeight: baseHeight + 1 + types.FeedActivationDelayBlocks,
			ResultingVersion:     types.InitialFeedVersion + 2,
		},
	)
}

// One batch is one version bump however many feeds it carries, so feeds
// scheduled in the same block report the same resulting version.
func (s *KeeperTestSuite) TestScheduleFeedTransitionSharesResultingVersionWithinBatch() {
	s.seedFeeds(feedUSD)
	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.FeedActivationDelayBlocks

	s.Require().NoError(s.scheduleAdd(feedGold))
	s.Require().NoError(s.scheduleAdd(feedSilver))

	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventFeedTransitionScheduled{
			Denom:                feedGold,
			Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
			ActivationVoteHeight: activationHeight,
			ResultingVersion:     types.InitialFeedVersion + 1,
		},
		&types.EventFeedTransitionScheduled{
			Denom:                feedSilver,
			Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
			ActivationVoteHeight: activationHeight,
			ResultingVersion:     types.InitialFeedVersion + 1,
		},
	)
}

// A batch that activates after this one has not promoted when this feed goes
// live, so it must not count towards the reported version. Genesis is the only
// writer that can leave a transition further out than a freshly scheduled one,
// since scheduling always lands a fixed delay from the current height.
func (s *KeeperTestSuite) TestScheduleFeedTransitionExcludesLaterPendingBatches() {
	baseHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight()
	imported := types.Feeds{
		Denoms:  []string{feedUSD},
		Version: types.InitialFeedVersion,
		Transitions: []types.FeedTransition{{
			Denom:                feedSilver,
			Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
			ActivationVoteHeight: baseHeight + 1000,
		}},
	}
	s.Require().NoError(imported.Validate())
	s.Require().NoError(s.keeper.Feeds.Set(s.ctx, imported))

	s.Require().NoError(s.scheduleAdd(feedGold))

	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventFeedTransitionScheduled{
			Denom:                feedGold,
			Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
			ActivationVoteHeight: baseHeight + types.FeedActivationDelayBlocks,
			ResultingVersion:     types.InitialFeedVersion + 1,
		},
	)
}

func (s *KeeperTestSuite) TestAdvanceFeedsWaitsForActivationHeight() {
	s.seedFeeds(feedUSD)
	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.FeedActivationDelayBlocks
	s.Require().NoError(s.scheduleAdd(feedGold))

	beforeCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(activationHeight - 1)
	s.Require().NoError(s.keeper.AdvanceFeeds(beforeCtx))

	feeds, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(feeds.Transitions, 1)
	s.Require().Equal(types.InitialFeedVersion, feeds.Version)
}

func (s *KeeperTestSuite) TestAdvanceFeedsPrunesRemovedRates() {
	s.seedFeeds(feedUSD, feedGold)
	for _, denom := range []string{feedUSD, feedGold} {
		s.Require().NoError(s.keeper.ExchangeRate.Set(
			s.ctx,
			denom,
			newStoredExchangeRate(denom, math.LegacyOneDec()),
		))
	}
	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.FeedActivationDelayBlocks
	s.Require().NoError(s.scheduleRemove(feedGold))

	activationCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(activationHeight)
	s.Require().NoError(s.keeper.AdvanceFeeds(activationCtx))

	promoted, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]string{feedUSD}, promoted.Denoms)

	// A rate that survived removal would be stale when the feed returns.
	hasRemoved, err := s.keeper.ExchangeRate.Has(s.ctx, feedGold)
	s.Require().NoError(err)
	s.Require().False(hasRemoved)

	hasSurviving, err := s.keeper.ExchangeRate.Has(s.ctx, feedUSD)
	s.Require().NoError(err)
	s.Require().True(hasSurviving)
}

func (s *KeeperTestSuite) TestAdvanceFeedsPromotesConsecutiveBatchesInOrder() {
	s.seedFeeds(feedUSD)
	baseHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight()

	s.Require().NoError(s.scheduleAdd(feedGold))

	// A second block schedules a batch one height later.
	nextCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(baseHeight + 1)
	s.Require().NoError(s.keeper.ScheduleFeedTransition(
		nextCtx,
		feedSilver,
		types.FeedDirection_FEED_DIRECTION_ADD,
	))

	feeds, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(feeds.Transitions, 2)
	s.Require().Equal(
		baseHeight+types.FeedActivationDelayBlocks,
		feeds.Transitions[0].ActivationVoteHeight,
	)
	s.Require().Equal(
		baseHeight+1+types.FeedActivationDelayBlocks,
		feeds.Transitions[1].ActivationVoteHeight,
	)

	// Both batches are overdue in this block: each advances the version once.
	lateCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(
		baseHeight + 1 + types.FeedActivationDelayBlocks,
	)
	s.Require().NoError(s.keeper.AdvanceFeeds(lateCtx))

	promoted, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(promoted.Transitions)
	s.Require().Equal(types.InitialFeedVersion+2, promoted.Version)
	s.Require().Equal([]string{feedGold, feedSilver, feedUSD}, promoted.Denoms)
}

func (s *KeeperTestSuite) TestAdvanceFeedsSupportsEmptyActiveSet() {
	s.seedFeeds(feedUSD)
	activationHeight := sdk.UnwrapSDKContext(s.ctx).BlockHeight() + types.FeedActivationDelayBlocks
	s.Require().NoError(s.scheduleRemove(feedUSD))

	activationCtx := sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(activationHeight)
	s.Require().NoError(s.keeper.AdvanceFeeds(activationCtx))

	promoted, err := s.keeper.Feeds.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(promoted.Denoms)
	s.Require().Equal(types.InitialFeedVersion+1, promoted.Version)
}
