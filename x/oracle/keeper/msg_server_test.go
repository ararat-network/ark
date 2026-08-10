package keeper_test

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestUpdateParams() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	s.Run("updates params", func() {
		params := types.DefaultParams()
		params.RewardWindow = 100
		params.RewardDistributionWindow = 1_000
		params.AttendanceWindow = 200
		s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(7)))
		s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 3, AttendedBlocks: 1}))

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().NoError(err)

		stored, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(uint64(100), stored.RewardWindow)

		accounting, err := s.keeper.Accounting.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(types.DefaultRewardWindow, accounting.RewardWindow)
		s.Require().Equal(types.DefaultRewardDistributionWindow, accounting.RewardDistributionWindow)
		s.Require().Equal(types.DefaultAttendanceWindow, accounting.AttendanceWindow)

		rewardWeight, err := s.keeper.RewardWeight.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().True(math.NewInt(7).Equal(rewardWeight))
		attendance, err := s.keeper.Attendance.Get(s.ctx, valAddr1)
		s.Require().NoError(err)
		s.Require().Equal(types.Attendance{EligibleBlocks: 3, AttendedBlocks: 1}, attendance)
	})

	s.Run("rejects invalid authority", func() {
		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: sdk.AccAddress("not-gov").String(),
			Params:    types.DefaultParams(),
		})
		s.Require().Error(err)
	})

	s.Run("rejects invalid params", func() {
		params := types.DefaultParams()
		params.RewardWindow = 0

		_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().ErrorContains(err, "RewardWindow must be between one and")
	})

	// Parameters carry no feed referents, so no parameter update can move
	// membership: that travels only through MsgAddFeed and MsgRemoveFeed, and a
	// scheduled removal in flight does not change the answer.
	s.Run("never touches feed membership", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{
			chain.USDBaseDenom,
			"aaud",
		})))
		s.Require().NoError(s.keeper.ScheduleFeedTransition(
			s.ctx,
			"aaud",
			types.FeedDirection_FEED_DIRECTION_REMOVE,
		))
		currentFeeds, err := s.keeper.Feeds.Get(s.ctx)
		s.Require().NoError(err)

		params := types.DefaultParams()
		params.RewardBand = math.LegacyNewDecWithPrec(5, 2)
		_, err = s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    params,
		})
		s.Require().NoError(err)

		storedParams, err := s.keeper.Params.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(params.RewardBand.Equal(storedParams.RewardBand))

		storedFeeds, err := s.keeper.Feeds.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(currentFeeds, storedFeeds)
	})
}

func (s *KeeperTestSuite) TestAddFeed() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	s.Run("schedules an addition", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{feedUSD})))

		_, err := s.msgServer.AddFeed(s.ctx, &types.MsgAddFeed{
			Authority: authority,
			Denom:     feedGold,
		})
		s.Require().NoError(err)

		feeds, err := s.keeper.Feeds.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Len(feeds.Transitions, 1)
		s.Require().Equal(feedGold, feeds.Transitions[0].Denom)
		s.Require().Equal(
			types.FeedDirection_FEED_DIRECTION_ADD,
			feeds.Transitions[0].Direction,
		)
	})

	s.Run("rejects invalid authority", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{feedUSD})))

		_, err := s.msgServer.AddFeed(s.ctx, &types.MsgAddFeed{
			Authority: sdk.AccAddress("not-gov").String(),
			Denom:     feedGold,
		})
		s.Require().Error(err)
	})

	s.Run("rejects an invalid feed denom", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{feedUSD})))

		_, err := s.msgServer.AddFeed(s.ctx, &types.MsgAddFeed{
			Authority: authority,
			Denom:     "aGOLD",
		})
		s.Require().ErrorContains(err, "must be an Ark-native base denom matching")
	})

	// Membership is declarative, so re-sending a live feed is how governance
	// re-approves one: nothing is scheduled and nothing changes.
	s.Run("restating a live feed schedules nothing", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{feedUSD, feedGold})))

		_, err := s.msgServer.AddFeed(s.ctx, &types.MsgAddFeed{
			Authority: authority,
			Denom:     feedGold,
		})
		s.Require().NoError(err)

		feeds, err := s.keeper.Feeds.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Empty(feeds.Transitions)
	})

	s.Run("rejects an addition conflicting with a scheduled removal", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{feedUSD, feedGold})))
		s.Require().NoError(s.scheduleRemove(feedGold))

		_, err := s.msgServer.AddFeed(s.ctx, &types.MsgAddFeed{
			Authority: authority,
			Denom:     feedGold,
		})
		s.Require().ErrorIs(err, types.ErrFeedTransitionPending)
	})
}

// Referent fixtures shared by the removal-guard and query tests. The strings
// mirror what x/asset actually reports, so the aggregation these exercise is
// the shape operators see.
const (
	consumerAsset  = "asset"
	consumerBasket = "basket"

	referentReference = "protocol reference"
	referentAssetUSD  = "asset ausd (ACTIVE)"
	referentBasket    = "component of abasket"
)

// stubFeedReferentGuard stands in for a future consumer guard (asset, basket,
// reserve) so the veto path keeps coverage while the wiring-owned set is
// empty. One stub reports every claim it holds for a denom, matching a real
// consumer that can pin a feed for more than one reason.
type stubFeedReferentGuard struct {
	consumer string
	pinned   map[string][]string
}

func (g stubFeedReferentGuard) FeedReferents(
	_ context.Context,
	denom string,
) ([]types.FeedReferent, error) {
	var referents []types.FeedReferent
	for _, referent := range g.pinned[denom] {
		referents = append(referents, types.FeedReferent{
			Consumer: g.consumer,
			Referent: referent,
		})
	}

	return referents, nil
}

func (s *KeeperTestSuite) TestRemoveFeed() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	s.Run("rejects removal while a registered guard pins the feed", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{chain.USDBaseDenom})))
		s.keeper.SetFeedReferentGuards(stubFeedReferentGuard{
			consumer: consumerAsset,
			pinned:   map[string][]string{chain.USDBaseDenom: {referentAssetUSD}},
		})

		_, err := s.msgServer.RemoveFeed(s.ctx, &types.MsgRemoveFeed{
			Authority: authority,
			Denom:     chain.USDBaseDenom,
		})
		s.Require().ErrorIs(err, types.ErrFeedReferenced)

		feeds, err := s.keeper.Feeds.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Empty(feeds.Transitions)
	})

	// Every blocker in one rejection: a proposal author fixing them serially
	// would burn a governance cycle per claim.
	s.Run("reports every claim across every guard in one rejection", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{chain.USDBaseDenom})))
		s.keeper.SetFeedReferentGuards(
			stubFeedReferentGuard{
				consumer: consumerAsset,
				pinned: map[string][]string{chain.USDBaseDenom: {
					referentReference,
					referentAssetUSD,
				}},
			},
			stubFeedReferentGuard{
				consumer: consumerBasket,
				pinned:   map[string][]string{chain.USDBaseDenom: {referentBasket}},
			},
		)

		_, err := s.msgServer.RemoveFeed(s.ctx, &types.MsgRemoveFeed{
			Authority: authority,
			Denom:     chain.USDBaseDenom,
		})
		s.Require().ErrorIs(err, types.ErrFeedReferenced)
		s.Require().ErrorContains(err, consumerAsset+": "+referentReference)
		s.Require().ErrorContains(err, consumerAsset+": "+referentAssetUSD)
		s.Require().ErrorContains(err, consumerBasket+": "+referentBasket)
	})

	s.Run("schedules removal once no guard pins the feed", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{
			chain.USDBaseDenom,
			feedGold,
		})))
		s.keeper.SetFeedReferentGuards(stubFeedReferentGuard{
			consumer: consumerAsset,
			pinned:   map[string][]string{chain.USDBaseDenom: {referentAssetUSD}},
		})

		_, err := s.msgServer.RemoveFeed(s.ctx, &types.MsgRemoveFeed{
			Authority: authority,
			Denom:     feedGold,
		})
		s.Require().NoError(err)

		feeds, err := s.keeper.Feeds.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Len(feeds.Transitions, 1)
		s.Require().Equal(feedGold, feeds.Transitions[0].Denom)
		s.Require().Equal(
			types.FeedDirection_FEED_DIRECTION_REMOVE,
			feeds.Transitions[0].Direction,
		)
	})

	// Scheduling treats removing an absent feed as a no-op, which would pass a
	// governance proposal that silently does nothing. The guard check rejects
	// it first so a typo or an already-removed feed fails where it is seen.
	s.Run("rejects removal of a feed that does not exist", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{chain.USDBaseDenom})))

		_, err := s.msgServer.RemoveFeed(s.ctx, &types.MsgRemoveFeed{
			Authority: authority,
			Denom:     feedGold,
		})
		s.Require().ErrorIs(err, types.ErrFeedNotFound)
	})

	// A feed already being removed still exists, so re-submitting inside the
	// activation window stays idempotent rather than becoming an error.
	s.Run("re-submitting an in-flight removal stays idempotent", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{feedGold})))

		for range 2 {
			_, err := s.msgServer.RemoveFeed(s.ctx, &types.MsgRemoveFeed{
				Authority: authority,
				Denom:     feedGold,
			})
			s.Require().NoError(err)
		}

		feeds, err := s.keeper.Feeds.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Len(feeds.Transitions, 1)
	})

	s.Run("removal is unguarded until consumers exist", func() {
		// The guard set is wiring-owned and empty on the pre-activation chain:
		// intermediate phase commits are not launchable configurations, so no
		// transitional guard protects the params-driven swap path.
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{chain.USDBaseDenom})))

		_, err := s.msgServer.RemoveFeed(s.ctx, &types.MsgRemoveFeed{
			Authority: authority,
			Denom:     chain.USDBaseDenom,
		})
		s.Require().NoError(err)
	})

	s.Run("rejects invalid authority", func() {
		s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{feedGold})))

		_, err := s.msgServer.RemoveFeed(s.ctx, &types.MsgRemoveFeed{
			Authority: sdk.AccAddress("not-gov").String(),
			Denom:     feedGold,
		})
		s.Require().Error(err)
	})
}
