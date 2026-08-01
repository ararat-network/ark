package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestRebaseTaxCap() {
	current := types.DefaultParams()
	current.ReferenceTaxCap = sdk.NewInt64Coin(chain.SDRBaseDenom, 100)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	// A stored per-denomination cap that must survive the rebase verbatim:
	// each derived cap is the reference value already expressed in its own
	// denomination — a unit-independent quantity — so re-expressing the
	// params coin does not touch the store.
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.KRWBaseDenom, math.NewInt(9)))

	// x/asset reads the pair once and hands it in; Treasury never reads the
	// oracle here — the strict mock carries that assertion.
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.SDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDec(2),
	}
	s.Require().NoError(s.keeper.RebaseTaxCap(s.ctx, chain.SDRBaseDenom, chain.USDBaseDenom, rates))

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	expected := current
	expected.ReferenceTaxCap = sdk.NewInt64Coin(chain.USDBaseDenom, 200)
	s.Require().Equal(expected, stored)
	survivingCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(9), survivingCap)
	s.requireTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: sdk.NewInt64Coin(chain.SDRBaseDenom, 100),
		NewCap: sdk.NewInt64Coin(chain.USDBaseDenom, 200),
	})
}

func (s *KeeperTestSuite) TestRebaseTaxCapToTheSameDenomIsANoOp() {
	current := types.DefaultParams()
	current.ReferenceTaxCap = sdk.NewInt64Coin(chain.SDRBaseDenom, 100)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

	// Landing on the unit the cap is already in has nothing to carry across:
	// no conversion — the nil set asserts that — no write, no event.
	s.Require().NoError(s.keeper.RebaseTaxCap(s.ctx, chain.SDRBaseDenom, chain.SDRBaseDenom, nil))

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(current, stored)
	s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
}

// TestRebaseTaxCapCarriesZeroWithoutConversion pins the uncapped sentinel's
// unit independence: zero means "no ceiling" in every denomination, so the
// rebase re-labels it without needing any rate — the nil set asserts no
// conversion happens — while still recording the move for auditability.
func (s *KeeperTestSuite) TestRebaseTaxCapCarriesZeroWithoutConversion() {
	current := types.DefaultParams()
	current.ReferenceTaxCap = sdk.NewInt64Coin(chain.SDRBaseDenom, 0)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))

	s.Require().NoError(s.keeper.RebaseTaxCap(s.ctx, chain.SDRBaseDenom, chain.USDBaseDenom, nil))

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.USDBaseDenom, 0), stored.ReferenceTaxCap)
	s.requireTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: sdk.NewInt64Coin(chain.SDRBaseDenom, 0),
		NewCap: sdk.NewInt64Coin(chain.USDBaseDenom, 0),
	})
}

func (s *KeeperTestSuite) TestRebaseTaxCapFailuresPreserveState() {
	tests := []struct {
		name      string
		from      string
		to        string
		rates     oracletypes.RateSet
		expectErr string
		errorIs   error
	}{
		{
			// x/asset owns the destination, so a `from` that disagrees with
			// Treasury's own cap denomination means the two modules disagree
			// about what unit the cap is in. Halting the proposal beats
			// silently capping tax in a unit the chain no longer prices
			// conversion in.
			name:      "from disagrees with the cap denomination",
			from:      chain.USDBaseDenom,
			to:        chain.KRWBaseDenom,
			rates:     nil,
			expectErr: "reference tax cap is denominated in asdr, not ausd",
			errorIs:   errortypes.ErrInvalidRequest,
		},
		{
			// A positive cap truncating to zero would silently delete the
			// finite ceiling: zero is the explicit uncapped sentinel, and
			// unlimited taxation by rounding accident is not a unit change.
			name: "positive cap truncates to zero",
			from: chain.SDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				chain.SDRBaseDenom:  math.LegacyNewDec(1_000),
				chain.USDBaseDenom:  math.LegacyOneDec(),
			},
			expectErr: "truncated to zero",
			errorIs:   oracletypes.ErrConversionOutOfRange,
		},
		{
			name: "destination has no rate in the handed set",
			from: chain.SDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				chain.SDRBaseDenom:  math.LegacyOneDec(),
			},
			expectErr: oracletypes.ErrUnknownDenom.Error(),
			errorIs:   oracletypes.ErrUnknownDenom,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			current := types.DefaultParams()
			current.ReferenceTaxCap = sdk.NewInt64Coin(chain.SDRBaseDenom, 100)
			s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
			eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

			err := s.keeper.RebaseTaxCap(s.ctx, tc.from, tc.to, tc.rates)
			s.Require().ErrorContains(err, tc.expectErr)
			s.Require().ErrorIs(err, tc.errorIs)

			stored, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(current, stored)
			s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
		})
	}
}
