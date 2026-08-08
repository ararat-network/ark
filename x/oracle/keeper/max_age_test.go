package keeper_test

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/oracle/types"
)

// A window governs its own denomination and nothing else. Everything without
// one — including a denomination the Oracle has never priced, and the
// numeraire, which has no feed at all — resolves to the chain default.
func (s *KeeperTestSuite) TestGetMaxAgeResolvesPerDenom() {
	tests := []struct {
		name  string
		denom string
		want  time.Duration
	}{
		{name: "overridden denom", denom: chain.SDRBaseDenom, want: 26 * time.Hour},
		{name: "other denom", denom: chain.USDBaseDenom, want: time.Minute},
		{name: "unpriced denom", denom: "aunknown", want: time.Minute},
		{name: "numeraire", denom: chain.NoahBaseDenom, want: time.Minute},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Seeded inside the subtest: the suite resets state around each one.
			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			params.MaxExchangeRateAge = time.Minute
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 26*time.Hour))

			maxAge, err := s.keeper.GetMaxAge(s.ctx, tc.denom)
			s.Require().NoError(err)
			s.Require().Equal(tc.want, maxAge)
		})
	}
}

// A raised default reaches every denomination that has no window of its own,
// and none that does: the two are independent knobs, not one folded into the
// other.
func (s *KeeperTestSuite) TestGetMaxAgeFollowsTheDefaultItFallsBackTo() {
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 26*time.Hour))

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Hour
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	overridden, err := s.keeper.GetMaxAge(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(26*time.Hour, overridden)

	inherited, err := s.keeper.GetMaxAge(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(time.Hour, inherited)
}

// SetMaxAge states the window rather than editing it: the result depends only
// on the value passed. A zero returns the denomination to the default whatever
// it carried before, and restating the stored value changes nothing.
func (s *KeeperTestSuite) TestSetMaxAgeIsDeclarative() {
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 26*time.Hour))
	stored, err := s.keeper.MaxExchangeRateAgeOverrides.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(26*time.Hour, stored)

	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 12*time.Hour))
	stored, err = s.keeper.MaxExchangeRateAgeOverrides.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(12*time.Hour, stored)

	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 0))
	found, err := s.keeper.MaxExchangeRateAgeOverrides.Has(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().False(found)

	maxAge, err := s.keeper.GetMaxAge(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultMaxExchangeRateAge, maxAge)
}

// Every window change is announced once, and only changes are announced.
// Restating a live feed is how governance re-approves one, so a restatement
// that moves nothing must not report a change that did not happen.
func (s *KeeperTestSuite) TestSetMaxAgeAnnouncesOnlyRealChanges() {
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 26*time.Hour))
	// Restated identically, then cleared, then cleared again from absent.
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 26*time.Hour))
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 0))
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 0))
	// A denomination that never carried a window is already at the default.
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.USDBaseDenom, 0))

	s.requireTypedEvents(
		sdk.UnwrapSDKContext(s.ctx).EventManager().Events(),
		&types.EventMaxExchangeRateAgeOverrideSet{
			Denom:  chain.SDRBaseDenom,
			MaxAge: 26 * time.Hour,
		},
		&types.EventMaxExchangeRateAgeOverrideRemoved{Denom: chain.SDRBaseDenom},
	)
}

// Zero is the documented way to ask for the default, so only a negative window
// is a malformed request.
func (s *KeeperTestSuite) TestSetMaxAgeRejectsNegative() {
	err := s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, -time.Second)
	s.Require().ErrorIs(err, types.ErrInvalidMaxExchangeRateAge)

	found, err := s.keeper.MaxExchangeRateAgeOverrides.Has(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().False(found)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

// A window must name a feed. Were a dangling one writable, nothing would ever
// read it and no removal could prune it — it would sit in state until an export
// produced a genesis that fails its own validation.
func (s *KeeperTestSuite) TestSetMaxAgeRequiresAFeed() {
	err := s.keeper.SetMaxAge(s.ctx, "agold", 26*time.Hour)
	s.Require().ErrorIs(err, types.ErrFeedNotFound)

	found, err := s.keeper.MaxExchangeRateAgeOverrides.Has(s.ctx, "agold")
	s.Require().NoError(err)
	s.Require().False(found)

	// A feed merely scheduled to join is feed enough: one AddFeed states
	// membership and window together, and the window has to land with it.
	s.Require().NoError(s.keeper.ScheduleFeedTransition(
		s.ctx,
		"agold",
		types.FeedDirection_FEED_DIRECTION_ADD,
	))
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, "agold", 26*time.Hour))

	maxAge, err := s.keeper.GetMaxAge(s.ctx, "agold")
	s.Require().NoError(err)
	s.Require().Equal(26*time.Hour, maxAge)
}

// Clearing is not gated on membership: feed removal prunes a departing feed's
// window, and by then the feed is already gone.
func (s *KeeperTestSuite) TestClearingMaxAgeNeedsNoFeed() {
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, "agold", 0))
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

// Genesis export and the query both read this, and both need key order.
func (s *KeeperTestSuite) TestGetMaxExchangeRateAgeOverridesInKeyOrder() {
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.USDBaseDenom, time.Hour))
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.SDRBaseDenom, 26*time.Hour))
	s.Require().NoError(s.keeper.SetMaxAge(s.ctx, chain.KRWBaseDenom, 2*time.Hour))

	overrides, err := s.keeper.GetMaxExchangeRateAgeOverrides(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]types.ExchangeRateAgeOverride{
		{Denom: chain.KRWBaseDenom, MaxAge: 2 * time.Hour},
		{Denom: chain.SDRBaseDenom, MaxAge: 26 * time.Hour},
		{Denom: chain.USDBaseDenom, MaxAge: time.Hour},
	}, overrides)
}

// The empty set exports as an empty slice rather than nil, which is what keeps
// an exported genesis byte-identical to one that was never touched.
func (s *KeeperTestSuite) TestGetMaxExchangeRateAgeOverridesEmpty() {
	overrides, err := s.keeper.GetMaxExchangeRateAgeOverrides(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]types.ExchangeRateAgeOverride{}, overrides)
}
