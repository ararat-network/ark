package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	chain "github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestRebaseReferenceState() {
	current := types.DefaultParams()
	current.ReferenceTaxCap = math.NewInt(100)
	current.MinBaseGasPrice = testMinBaseGasPrice
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	// A stored factor is reference-relative, so the rebase must re-express it
	// in the new unit for the derived cap — a unit-independent quantity — to
	// survive the re-point verbatim.
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, types.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: math.LegacyNewDec(9),
	}))
	capBefore, err := s.keeper.GetTaxCap(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(900), capBefore)
	// The NOAH cross is reference-relative too and rescales in the table.
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, types.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

	// x/asset reads the pair once and hands it in; Treasury never reads the
	// oracle here — the strict mock carries that assertion.
	// One XDR is one NOAH and one USD is half a NOAH, so one XDR is two USD.
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDecWithPrec(5, 1),
	}
	s.Require().NoError(s.keeper.RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, rates))

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	expected := current
	expected.ReferenceDenom = chain.USDBaseDenom
	expected.ReferenceTaxCap = math.NewInt(200)
	// The base-fee floor and live price are reference-quoted too, so both
	// re-quote through the same cross — one XDR is two USD here.
	expected.MinBaseGasPrice = math.LegacyMustNewDecFromStr("0.2")
	s.Require().Equal(expected, stored)
	price, err := s.keeper.BaseGasPrice.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.2"), price)
	// One USD is half an XDR at these rates, so the factor halves while the
	// derived cap survives the unit change untouched.
	rescaled, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("4.5"), rescaled.Factor)
	capAfter, err := s.keeper.GetTaxCap(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(900), capAfter)
	noahCross, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.5"), noahCross.Factor)
	s.requireTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: sdk.NewInt64Coin(chain.XDRBaseDenom, 100),
		NewCap: sdk.NewInt64Coin(chain.USDBaseDenom, 200),
	})
	// The re-quote and the rescale report through the states' own streams —
	// refresh emits on change only, so these writes are the streams' record
	// of the unit change.
	s.requireTypedEvent(&types.EventBaseGasPriceUpdated{
		BaseGasPrice: math.LegacyMustNewDecFromStr("0.2"),
	})
	height := uint64(sdk.UnwrapSDKContext(s.ctx).BlockHeight())
	s.requireTypedEvent(&types.EventConversionFactorsRefreshed{
		ConversionFactors: []types.ConversionFactor{
			{Denom: chain.KRWBaseDenom, Factor: math.LegacyMustNewDecFromStr("4.5"), DerivedHeight: height},
			{Denom: chain.NoahBaseDenom, Factor: math.LegacyMustNewDecFromStr("0.5"), DerivedHeight: height},
		},
	})
}

func (s *KeeperTestSuite) TestRebaseReferenceStateToTheSameDenomIsANoOp() {
	current := types.DefaultParams()
	current.ReferenceTaxCap = math.NewInt(100)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

	// Landing on the unit the cap is already in has nothing to carry across:
	// no conversion — the nil set asserts that — no write, no event.
	s.Require().NoError(s.keeper.RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.XDRBaseDenom, nil))

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(current, stored)
	s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
}

// TestRebaseReferenceStateCarriesZeroWithoutConversion pins the uncapped sentinel's
// unit independence: zero means "no ceiling" in every denomination, so the
// cap re-labels without a conversion touching its amount, while the base-fee
// figures — always positive — convert through the handed cross.
func (s *KeeperTestSuite) TestRebaseReferenceStateCarriesZeroWithoutConversion() {
	current := types.DefaultParams()
	current.ReferenceTaxCap = math.NewInt(0)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, types.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: math.LegacyNewDec(9),
	}))
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDecWithPrec(5, 1),
	}

	s.Require().NoError(s.keeper.RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, rates))

	// The factor rescales through the cross like any member's — the zero cap
	// exempts only the cap's own amount from conversion.
	rescaled, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("4.5"), rescaled.Factor)

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, stored.ReferenceDenom)
	s.Require().Equal(math.ZeroInt(), stored.ReferenceTaxCap)
	s.requireTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: sdk.NewInt64Coin(chain.XDRBaseDenom, 0),
		NewCap: sdk.NewInt64Coin(chain.USDBaseDenom, 0),
	})
}

// TestRebaseReferenceStateFloorsTruncatedCapAtOneUnit pins the degrade at a unit
// change: a positive cap whose conversion truncates below one base unit lands
// at one — the tightest finite ceiling — because zero is the explicit
// uncapped sentinel, and refusing outright would wedge the reference re-point
// on a rate pair no retry can mend.
func (s *KeeperTestSuite) TestRebaseReferenceStateFloorsTruncatedCapAtOneUnit() {
	current := types.DefaultParams()
	current.ReferenceTaxCap = math.NewInt(100)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	// One USD is a thousand NOAH and one XDR is one, so the cap of a hundred
	// XDR is a tenth of a USD.
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDec(1_000),
	}

	s.Require().NoError(s.keeper.RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, rates))

	stored, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, stored.ReferenceDenom)
	s.Require().Equal(math.OneInt(), stored.ReferenceTaxCap)
	s.requireTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: sdk.NewInt64Coin(chain.XDRBaseDenom, 100),
		NewCap: sdk.NewInt64Coin(chain.USDBaseDenom, 1),
	})
}

// TestRebaseReferenceStateUnrepresentableRescaleHoldsThatFactor pins the
// per-member degrade for a product past the Dec domain: the member whose
// factor cannot be re-expressed holds its old-unit value — refresh's own
// degrade for the same figure — while every fitting entry, NOAH included,
// still moves and the re-point itself proceeds.
func (s *KeeperTestSuite) TestRebaseReferenceStateUnrepresentableRescaleHoldsThatFactor() {
	// A 10^60 factor times the 10^18 cross leaves the Dec domain, while the
	// inverse unit cross stays exactly the smallest representable positive, so
	// the base-fee leg converts and only this member's rescale degrades.
	huge := math.LegacyNewDec(10).Power(60)
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, types.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: huge,
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.EURBaseDenom, types.ConversionFactor{
		Denom:  chain.EURBaseDenom,
		Factor: math.LegacyNewDec(2),
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, types.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

	cross := math.LegacyNewDec(10).Power(18)
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  cross,
	}
	s.Require().NoError(s.keeper.RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, rates))

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, params.ReferenceDenom)
	held, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(huge, held.Factor)
	moved, err := s.keeper.ConversionFactors.Get(s.ctx, chain.EURBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(2).Mul(cross), moved.Factor)
	noah, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(cross, noah.Factor)
}

// TestRebaseReferenceStateSubPrecisionRescaleHoldsThatFactor is the underflow
// mirror: a product rounding to zero is held rather than written, because a
// zero factor is a state genesis validation refuses — the export could never
// re-import. Fitting entries still move.
func (s *KeeperTestSuite) TestRebaseReferenceStateSubPrecisionRescaleHoldsThatFactor() {
	small := math.LegacyMustNewDecFromStr("0.4")
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, types.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: small,
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.EURBaseDenom, types.ConversionFactor{
		Denom:  chain.EURBaseDenom,
		Factor: math.LegacyNewDec(2),
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, types.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

	// cross = 10^-18: 0.4 x cross rounds to zero and holds; 2 x cross and
	// 1 x cross land exactly on the smallest representable positives.
	cross := math.LegacySmallestDec()
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
		chain.USDBaseDenom:  cross,
	}
	s.Require().NoError(s.keeper.RebaseReferenceState(s.ctx, chain.XDRBaseDenom, chain.USDBaseDenom, rates))

	held, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(small, held.Factor)
	moved, err := s.keeper.ConversionFactors.Get(s.ctx, chain.EURBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(2).Mul(cross), moved.Factor)
	noah, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(cross, noah.Factor)
}

func (s *KeeperTestSuite) TestRebaseReferenceStateFailuresPreserveState() {
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
			expectErr: "treasury reference denom is axdr, not ausd",
			errorIs:   errortypes.ErrInvalidRequest,
		},
		{
			name: "destination has no rate in the handed set",
			from: chain.XDRBaseDenom,
			to:   chain.USDBaseDenom,
			rates: oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				chain.XDRBaseDenom:  math.LegacyOneDec(),
			},
			expectErr: oracletypes.ErrUnknownDenom.Error(),
			errorIs:   oracletypes.ErrUnknownDenom,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			current := types.DefaultParams()
			current.ReferenceTaxCap = math.NewInt(100)
			s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
			eventsBefore := len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

			err := s.keeper.RebaseReferenceState(s.ctx, tc.from, tc.to, tc.rates)
			s.Require().ErrorContains(err, tc.expectErr)
			s.Require().ErrorIs(err, tc.errorIs)

			stored, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(current, stored)
			s.Require().Len(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), eventsBefore)
		})
	}
}
