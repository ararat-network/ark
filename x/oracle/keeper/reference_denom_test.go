package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/types"
)

// seedReferenceDenomRates stores fresh rates for the denominations a rebase prices,
// so a reference move reads real state rather than a stubbed pair.
func (s *KeeperTestSuite) seedReferenceDenomRates(rates map[string]math.LegacyDec) {
	for denom, rate := range rates {
		s.Require().NoError(s.keeper.ExchangeRate.Set(
			s.ctx,
			denom,
			newStoredExchangeRate(denom, rate),
		))
	}
}

func (s *KeeperTestSuite) TestGetReferenceDenomDefaultsEmpty() {
	referenceDenom, err := s.keeper.GetReferenceDenom(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(referenceDenom)
}

func (s *KeeperTestSuite) TestSetReferenceDenomFirstConfigurationSkipsRebase() {
	// First configuration has no outgoing reference denom to rebase away from, so no
	// executor expectations are registered and any call would fail the test.
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}

// TestSetReferenceDenomAcceptsAnyActiveFeed pins what the reference actually names:
// a feed. Both consumers only ever read a rate, so no asset registry is
// consulted — a reference unit need not be a listed asset anywhere.
func (s *KeeperTestSuite) TestSetReferenceDenomAcceptsAnyActiveFeed() {
	s.seedFeeds(chain.XDRBaseDenom, chain.USDBaseDenom, feedGold)

	tests := []struct {
		name  string
		denom string
	}{
		// The launch reference denom: the configuration the chain ships with.
		{name: "launch reference denom feed", denom: chain.XDRBaseDenom},
		{name: "stablecoin feed", denom: chain.USDBaseDenom},
		{name: "feed with no listed asset", denom: feedGold},
	}
	for _, test := range tests {
		s.Run(test.name, func() {
			s.seedFeeds(chain.XDRBaseDenom, chain.USDBaseDenom, feedGold)

			// Each case configures the reference from nothing, so no rebase
			// runs and any executor call would fail the test.
			s.Require().NoError(s.keeper.ReferenceDenom.Remove(s.ctx))

			s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, test.denom, math.LegacyDec{}))
			s.requireStoredReferenceDenom(test.denom)
		})
	}
}

func (s *KeeperTestSuite) TestSetReferenceDenomRejectsIneligibleDenoms() {
	tests := []struct {
		name          string
		setup         func()
		denom         string
		expectMessage string
	}{
		// Clearing a configured reference is the same rejection as setting an
		// empty one: consumers hold state denominated in it.
		{
			name:          "empty reference denom",
			denom:         "",
			expectMessage: "reference denom must be set",
		},
		// The numeraire is excluded for the same reason it has no feed.
		{
			name:          "numeraire",
			denom:         chain.NoahBaseDenom,
			expectMessage: "is the numeraire and is never priced",
		},
		{
			name:          "malformed denom",
			denom:         "AXDR",
			expectMessage: "must be an Ark-native base denom",
		},
		// An Adding feed has no rate yet, and a Removing feed was already
		// cleared of referents by the removal guard: naming the reference denom
		// creates a referent and must not race that window.
		{
			name: "feed being added",
			setup: func() {
				s.seedFeeds(chain.XDRBaseDenom)
				s.Require().NoError(s.scheduleAdd(feedSilver))
			},
			denom:         feedSilver,
			expectMessage: "is not active",
		},
		{
			name: "feed being removed",
			setup: func() {
				s.seedFeeds(chain.XDRBaseDenom, feedGold)
				s.Require().NoError(s.scheduleRemove(feedGold))
			},
			denom:         feedGold,
			expectMessage: "is not active",
		},
		{
			name:          "feed absent",
			denom:         "azinc",
			expectMessage: "is not active",
		},
	}
	for _, test := range tests {
		s.Run(test.name, func() {
			if test.setup != nil {
				test.setup()
			}

			err := s.keeper.SetReferenceDenom(s.ctx, test.denom, math.LegacyDec{})
			s.Require().ErrorIs(err, types.ErrInvalidReferenceDenom)
			s.Require().ErrorContains(err, test.expectMessage)
		})
	}
}

// TestSetReferenceDenomChangeRebasesConsumers pins the executor rate contract: both
// sides of the pair are priced once here through a single freshness-checked
// read, and both executors receive the identical merged set.
func (s *KeeperTestSuite) TestSetReferenceDenomChangeRebasesConsumers() {
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	incoming := math.LegacyNewDec(2)
	outgoing := math.LegacyNewDecWithPrec(15, 1)
	s.seedReferenceDenomRates(map[string]math.LegacyDec{
		chain.USDBaseDenom: incoming,
		chain.XDRBaseDenom: outgoing,
	})
	handed := types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  incoming,
		chain.XDRBaseDenom:  outgoing,
	}

	s.marketReferenceDenom.EXPECT().
		RebaseBasePool(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, handed).
		Return(nil)
	s.treasuryReferenceDenom.EXPECT().
		RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, handed).
		Return(nil)

	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.USDBaseDenom, math.LegacyDec{}))

	s.requireStoredReferenceDenom(chain.USDBaseDenom)
}

// TestSetReferenceDenomChangeRequiresFreshIncomingRate pins the fresh half of the
// contract: a successor the chain is not currently pricing is no recovery at
// all, so an unpriced incoming rate fails the whole action before any executor
// runs. The strict executor mocks registering no expectations is the assertion.
func (s *KeeperTestSuite) TestSetReferenceDenomChangeRequiresFreshIncomingRate() {
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	// Only the outgoing side is priced, so the incoming denomination fails.
	s.seedReferenceDenomRates(map[string]math.LegacyDec{
		chain.XDRBaseDenom: math.LegacyOneDec(),
	})

	err := s.keeper.SetReferenceDenom(s.ctx, chain.USDBaseDenom, math.LegacyDec{})
	s.Require().ErrorIs(err, types.ErrReferenceDenomRebaseUnavailable)
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}

// TestSetReferenceDenomChangeRequiresPriceableOutgoingRate pins the other half: an
// outgoing unit the chain cannot currently price — stale, pruned, or never
// valued — fails the action rather than converting consumer state at whatever
// the store last held. Supplying a rate is the escape, covered below.
func (s *KeeperTestSuite) TestSetReferenceDenomChangeRequiresPriceableOutgoingRate() {
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	// The outgoing denomination is part of the priced set, so it never having
	// been valued is the whole action's failure.
	s.seedReferenceDenomRates(map[string]math.LegacyDec{
		chain.USDBaseDenom: math.LegacyNewDec(2),
	})

	err := s.keeper.SetReferenceDenom(s.ctx, chain.USDBaseDenom, math.LegacyDec{})
	s.Require().ErrorIs(err, types.ErrReferenceDenomRebaseUnavailable)
	s.Require().ErrorContains(err, "pricing reference denom move")
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}

// TestSetReferenceDenomSuppliedRateSkipsOutgoingPricing checks that an explicit outgoing rate
// permits a rebase without reading or pricing the outgoing feed.
func (s *KeeperTestSuite) TestSetReferenceDenomSuppliedRateSkipsOutgoingPricing() {
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	incoming := math.LegacyNewDec(2)
	supplied := math.LegacyNewDecWithPrec(15, 1)
	s.seedReferenceDenomRates(map[string]math.LegacyDec{
		chain.USDBaseDenom: incoming,
	})
	handed := types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  incoming,
		chain.XDRBaseDenom:  supplied,
	}

	// Both executors must receive the supplied rate and the same set: a rebase
	// converting Market and Treasury at different rates would leave the two
	// disagreeing about the unit.
	s.marketReferenceDenom.EXPECT().
		RebaseBasePool(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, handed).
		Return(nil)
	s.treasuryReferenceDenom.EXPECT().
		RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, handed).
		Return(nil)

	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.USDBaseDenom, supplied))

	s.requireStoredReferenceDenom(chain.USDBaseDenom)
}

// Re-setting the reference to the denomination it already names must not
// rebase: consumer state is already denominated in it, so running the
// executors would re-denominate it a second time.
func (s *KeeperTestSuite) TestSetReferenceDenomUnchangedDenomSkipsRebase() {
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	// No executor expectations are registered, so a rebase would fail here.
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}

func (s *KeeperTestSuite) TestSetReferenceDenomChangeWithoutExecutorsFails() {
	s.keeper.SetReferenceDenomConsumers(nil, nil)
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	err := s.keeper.SetReferenceDenom(s.ctx, chain.USDBaseDenom, math.LegacyDec{})
	s.Require().ErrorIs(err, types.ErrReferenceDenomRebaseUnavailable)
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}

func (s *KeeperTestSuite) TestSetReferenceDenomExecutorErrorFailsAtomically() {
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	s.seedReferenceDenomRates(map[string]math.LegacyDec{
		chain.USDBaseDenom: math.LegacyNewDec(2),
		chain.XDRBaseDenom: math.LegacyOneDec(),
	})
	s.marketReferenceDenom.EXPECT().
		RebaseBasePool(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, gomock.Any()).
		Return(errors.New("pool rebase failed"))

	err := s.keeper.SetReferenceDenom(s.ctx, chain.USDBaseDenom, math.LegacyDec{})
	s.Require().ErrorIs(err, types.ErrReferenceDenomRebaseUnavailable)
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}

// TestFeedReferentsPinTheProtocolReferenceDenom covers oracle's own claim, which is
// independent of any consumer: both consumers price against the unit
// continuously, so the feed cannot leave while it is named.
func (s *KeeperTestSuite) TestFeedReferentsPinTheProtocolReferenceDenom() {
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	resp, err := s.queryClient.FeedReferents(s.ctx, &types.QueryFeedReferentsRequest{
		Denom: chain.XDRBaseDenom,
	})
	s.Require().NoError(err)
	s.Require().Equal(
		[]types.FeedReferent{{
			Consumer: types.ModuleName,
			Referent: "protocol reference denom",
		}},
		resp.Referents,
	)
}

// An unnamed feed is unpinned by the reference denom: the claim tracks the one
// denomination, not the registry it lives in.
func (s *KeeperTestSuite) TestFeedReferentsLeaveUnnamedFeedsUnpinned() {
	s.seedFeeds(chain.XDRBaseDenom, chain.USDBaseDenom)
	s.Require().NoError(s.keeper.SetReferenceDenom(s.ctx, chain.XDRBaseDenom, math.LegacyDec{}))

	resp, err := s.queryClient.FeedReferents(s.ctx, &types.QueryFeedReferentsRequest{
		Denom: chain.USDBaseDenom,
	})
	s.Require().NoError(err)
	s.Require().Empty(resp.Referents)
}

func (s *KeeperTestSuite) requireStoredReferenceDenom(expected string) {
	stored, err := s.keeper.GetReferenceDenom(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected, stored)
}

func (s *KeeperTestSuite) TestMsgSetReferenceDenom() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	_, err := s.msgServer.SetReferenceDenom(s.ctx, &types.MsgSetReferenceDenom{
		Authority:      sdk.AccAddress("not-gov").String(),
		ReferenceDenom: chain.XDRBaseDenom,
	})
	s.Require().Error(err)

	_, err = s.msgServer.SetReferenceDenom(s.ctx, &types.MsgSetReferenceDenom{
		Authority:      authority,
		ReferenceDenom: chain.XDRBaseDenom,
	})
	s.Require().NoError(err)
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}

// TestMsgSetReferenceDenomOutgoingRateValidation checks absent and zero overrides both use ordinary
// pricing, including Amino round trips, and rejects negative or over-cap rates.
func (s *KeeperTestSuite) TestMsgSetReferenceDenomOutgoingRateValidation() {
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	_, err := s.msgServer.SetReferenceDenom(s.ctx, &types.MsgSetReferenceDenom{
		Authority:      authority,
		ReferenceDenom: chain.XDRBaseDenom,
	})
	s.Require().NoError(err)

	// A negative rate is refused before any executor runs, so the strict mocks
	// registering no expectations is itself part of the assertion.
	_, err = s.msgServer.SetReferenceDenom(s.ctx, &types.MsgSetReferenceDenom{
		Authority:      authority,
		ReferenceDenom: chain.USDBaseDenom,
		OutgoingRate:   math.LegacyNewDec(-1),
	})
	s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
	s.Require().ErrorContains(err, "outgoing reference denom rate must be positive")
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)

	// Past the domain cap is refused at the same write: the rate joins the
	// handed set the consumers' rescale arithmetic reads, so the cap is the
	// human-in-the-loop defence and the checked multiply behind it stays a
	// backstop.
	_, err = s.msgServer.SetReferenceDenom(s.ctx, &types.MsgSetReferenceDenom{
		Authority:      authority,
		ReferenceDenom: chain.USDBaseDenom,
		OutgoingRate:   types.MaxOutgoingReferenceRate.Add(math.LegacyOneDec()),
	})
	s.Require().ErrorIs(err, errortypes.ErrInvalidRequest)
	s.Require().ErrorContains(err, "at most")
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)

	// Zero reads as absent, so this prices the outgoing denomination and fails
	// there rather than on validation — the same way an omitted field would.
	// Only the incoming side is priced, leaving the outgoing read to fail.
	s.seedReferenceDenomRates(map[string]math.LegacyDec{
		chain.USDBaseDenom: math.LegacyNewDec(2),
	})

	_, err = s.msgServer.SetReferenceDenom(s.ctx, &types.MsgSetReferenceDenom{
		Authority:      authority,
		ReferenceDenom: chain.USDBaseDenom,
		OutgoingRate:   math.LegacyZeroDec(),
	})
	s.Require().ErrorIs(err, types.ErrReferenceDenomRebaseUnavailable)
	s.requireStoredReferenceDenom(chain.XDRBaseDenom)
}
