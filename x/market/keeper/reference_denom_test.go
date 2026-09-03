package keeper_test

import (
	"strings"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestRebaseBasePool() {
	current := types.DefaultConversionPolicy()
	current.BasePool = xdrBasePool(math.LegacyNewDec(100))
	oldDelta := math.LegacyNewDec(25)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))

	// x/asset reads the rate pair once for both reference consumers and hands
	// the set in, so Market converts without an oracle read of its own. The
	// absent GetRateSet expectation asserts exactly that. One XDR is one NOAH
	// and one USD is half a NOAH, so one XDR is two USD.
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDecWithPrec(5, 1),
	}

	s.Require().NoError(s.keeper.RebaseBasePool(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, rates))

	// The depth is a claim about how much conversion the protocol absorbs before
	// the spread widens, expressed in reference units, so it is carried across at
	// the current rate. The delta scales with it: a re-denomination changes the
	// unit, not the economic stance, so the effective pools keep their ratio.
	rebased := sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyNewDec(200))
	newDelta := math.LegacyNewDec(50)
	expectedCapacity := current
	expectedCapacity.BasePool = rebased
	storedCapacity, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expectedCapacity, storedCapacity)
	storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(newDelta.Equal(storedDelta), "expected delta %s, got %s", newDelta, storedDelta)

	s.requirePoolUpdateEvent(current.BasePool, rebased, oldDelta, newDelta)
}

func (s *KeeperTestSuite) TestRebaseBasePoolToTheSameDenomIsANoOp() {
	current := types.DefaultConversionPolicy()
	current.BasePool = xdrBasePool(math.LegacyNewDec(100))
	oldDelta := math.LegacyNewDec(25)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))
	eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

	// A reference that lands on the unit the pool is already in has nothing to
	// carry across, so the handed rates are never consulted — the nil set
	// asserts that — and neither the depth nor the delta is rewritten.
	s.Require().NoError(s.keeper.RebaseBasePool(s.ctx, chain.XDRBaseDenom, chain.XDRBaseDenom, nil))

	storedCapacity, err := s.keeper.ConversionPolicy.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(current, storedCapacity)
	storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(oldDelta.Equal(storedDelta))
	s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
}

func (s *KeeperTestSuite) TestRebaseBasePoolFailuresPreserveState() {
	// The handed set never carries a staleness or internal-lookup error the way
	// the old GetRateSet call could: x/asset decides freshness when it captures
	// the pair. What the new contract can produce is a missing rate — Convert
	// reports ErrUnknownDenom, which Market maps to ErrNoEffectivePrice — and an
	// internal conversion failure, which must pass through unmapped.
	tests := []struct {
		name       string
		from       string
		to         string
		rates      oracletypes.RateSet
		expectErr  string
		errorIs    error
		errorIsNot error
	}{
		{
			// x/asset owns the destination, so a `from` that disagrees with
			// Market's own pool denomination means the two modules disagree about
			// what unit the pool is in. Halting the proposal is the only safe
			// answer: silently re-anchoring mis-sizes every subsequent swap.
			name:      "from disagrees with the pool denomination",
			from:      chain.USDBaseDenom,
			to:        chain.KRWBaseDenom,
			rates:     nil,
			expectErr: "base pool is denominated in axdr, not ausd",
			errorIs:   errortypes.ErrInvalidRequest,
		},
		{
			name: "outgoing reference is missing from the handed set",
			from: chain.XDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				chain.USDBaseDenom:  math.LegacyNewDec(2),
			},
			expectErr: oracletypes.ErrUnknownDenom.Error(),
			errorIs:   types.ErrNoEffectivePrice,
		},
		{
			name: "destination has no price in the handed set",
			from: chain.XDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				chain.XDRBaseDenom:  math.LegacyOneDec(),
			},
			expectErr: oracletypes.ErrUnknownDenom.Error(),
			errorIs:   types.ErrNoEffectivePrice,
		},
		{
			// An internal conversion failure is not a missing price, and
			// reporting it as one would tell governance to configure a feed that
			// already exists.
			name: "conversion through the handed set overflows",
			from: chain.XDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				// Near the LegacyDec ceiling (~1.16e77), so valuing the 100-unit
				// pool in NOAH is unrepresentable and the multiply inside Convert
				// fails before any state is touched.
				chain.XDRBaseDenom: math.LegacyMustNewDecFromStr("1" + strings.Repeat("0", 76)),
				chain.USDBaseDenom: math.LegacyOneDec(),
			},
			expectErr:  oracletypes.ErrConversionOutOfRange.Error(),
			errorIs:    oracletypes.ErrConversionOutOfRange,
			errorIsNot: types.ErrNoEffectivePrice,
		},
		{
			// Zero is a valid conversion result but never a swappable depth:
			// a pool that truncates to nothing is caught by the effective-pool
			// guard on the candidate, not by the conversion.
			name: "rebased depth truncates to zero",
			from: chain.XDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				chain.XDRBaseDenom:  math.LegacySmallestDec(),
				chain.USDBaseDenom:  math.LegacyNewDec(1_000),
			},
			expectErr:  "invalid effective pools after rebasing to ausd",
			errorIs:    errortypes.ErrInvalidRequest,
			errorIsNot: types.ErrNoEffectivePrice,
		},
		{
			// The conversion itself can succeed while the rebased depth is too
			// deep to ever swap against: the effective-pool guard runs on the
			// candidate before anything is written.
			name: "rebased depth fails effective-pool validation",
			from: chain.XDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				// Representable after conversion, but the constant product the
				// swap math needs squares it out of range.
				chain.XDRBaseDenom: math.LegacyMustNewDecFromStr("1" + strings.Repeat("0", 59)),
				chain.USDBaseDenom: math.LegacyOneDec(),
			},
			expectErr:  "invalid effective pools after rebasing to ausd",
			errorIs:    errortypes.ErrInvalidRequest,
			errorIsNot: types.ErrNoEffectivePrice,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			current := types.DefaultConversionPolicy()
			current.BasePool = xdrBasePool(math.LegacyNewDec(100))
			oldDelta := math.LegacyNewDec(25)
			s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, current))
			s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, oldDelta))
			eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

			err := s.keeper.RebaseBasePool(s.ctx, tc.from, tc.to, tc.rates)
			s.Require().ErrorContains(err, tc.expectErr)
			s.Require().ErrorIs(err, tc.errorIs)
			if tc.errorIsNot != nil {
				s.Require().NotErrorIs(err, tc.errorIsNot)
			}

			storedCapacity, err := s.keeper.ConversionPolicy.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(current, storedCapacity)
			storedDelta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(oldDelta.Equal(storedDelta))
			s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
		})
	}
}
