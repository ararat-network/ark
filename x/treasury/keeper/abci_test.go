package keeper_test

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// TestEndBlockerAccruesRewardFunding pins the accrual's phase: at EndBlock the
// collector holds the block's own fees, so the first real block already
// accrues — there is no genesis-height skip to inherit from the BeginBlock
// era — and a member's fee values through the registry's oracle-priced
// membership, not any Treasury- or Oracle-owned list.
func (s *KeeperTestSuite) TestEndBlockerAccruesRewardFunding() {
	tests := []struct {
		name   string
		height int64
		fees   sdk.Coins
		rates  oracletypes.RateSet
		value  int64
	}{
		{
			name:   "the first block",
			height: 1,
			fees:   sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 3)),
			value:  3,
		},
		{
			name:   "a later block",
			height: 2,
			fees:   sdk.NewCoins(),
		},
		{
			name:   "a member's fee",
			height: 2,
			fees:   sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 5)),
			rates:  oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()},
			value:  5,
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setBlockHeight(tc.height)
			s.setRates(tc.rates)
			s.expectValidatorFees(tc.fees)

			s.Require().NoError(s.endBlock())
			funding, err := s.keeper.RewardFunding.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
			s.Require().Equal(math.NewInt(tc.value), funding.ValidatorFeeValue)
		})
	}
}

// TestBeginBlockerValuesNoLiability checks that BeginBlock off the exposure cadence never reads
// supply or folds liability, even with values that would fail valuation.
func (s *KeeperTestSuite) TestBeginBlockerValuesNoLiability() {
	s.setBlockHeight(2)
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(1, 18),
	})

	s.Require().NoError(s.beginBlock())
	s.requireNoTypedEvent(&types.EventLiabilityIncomplete{})
}

// TestEndBlockerDerivesFactorsEveryBlock pins the per-block contract: a
// servable member's factor tracks this block's rate, the derived cap tracks
// the factor, and the changed-only event stays quiet on a block that
// re-derives every factor to the value it already held.
func (s *KeeperTestSuite) TestEndBlockerDerivesFactorsEveryBlock() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	// One XDR is one NOAH and one USD is half a NOAH, so one XDR is two USD.
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(5, 1),
	})

	s.Require().NoError(s.endBlockWithoutFees())

	usdCap, err := s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)
	xdrCap, err := s.keeper.GetTaxCap(s.ctx, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), xdrCap)
	s.requireTypedEvent(&types.EventConversionFactorsRefreshed{ConversionFactors: []types.ConversionFactor{
		{Denom: chain.USDBaseDenom, Factor: math.LegacyNewDec(2), DerivedHeight: 1},
		{Denom: chain.XDRBaseDenom, Factor: math.LegacyOneDec(), DerivedHeight: 1},
		// The NOAH cross derives in the same pass and reports last.
		{Denom: chain.NoahBaseDenom, Factor: math.LegacyOneDec(), DerivedHeight: 1},
	}})

	// An unchanged block writes nothing and says nothing.
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithEventManager(sdk.NewEventManager())
	s.Require().NoError(s.endBlockWithoutFees())
	s.requireNoTypedEvent(&types.EventConversionFactorsRefreshed{})
	usd, err := s.keeper.ConversionFactors.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), usd.DerivedHeight)

	// A moved rate re-derives the one factor it moved.
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(25, 2),
	})
	s.Require().NoError(s.endBlockWithoutFees())
	usdCap, err = s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(4_000_000), usdCap)
	s.requireTypedEvent(&types.EventConversionFactorsRefreshed{ConversionFactors: []types.ConversionFactor{
		{Denom: chain.USDBaseDenom, Factor: math.LegacyNewDec(4), DerivedHeight: 1},
	}})
}

// TestEndBlockerKeepsFactorThroughOutage proves a dark feed keeps its last
// factor, the derived cap keeps clamping through the outage, and the pass
// mends the factor the block the feed returns — every block is the retry.
func (s *KeeperTestSuite) TestEndBlockerKeepsFactorThroughOutage() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.TransferTaxRate = math.LegacyMustNewDecFromStr("0.1")
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.KRWBaseDenom, chain.XDRBaseDenom)
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, types.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: math.LegacyMustNewDecFromStr("0.000007"),
	}))

	// The outage block: no rate serves akrw, so its factor is kept untouched.
	s.Require().NoError(s.endBlockWithoutFees())
	kept, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.000007"), kept.Factor)
	s.Require().Equal(uint64(0), kept.DerivedHeight)

	// Every member taxes through the outage: the kept factor keeps deriving
	// the cap that clamps — 10 would be owed at the rate, the derived cap of
	// 7 binds. An oracle outage must not make a healthy denomination
	// untransactable — nor tax-exempt.
	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 7)), tax)

	// The feed returns and the very next block re-derives.
	s.setRates(oracletypes.RateSet{
		chain.KRWBaseDenom: math.LegacyNewDecWithPrec(5, 1),
		chain.XDRBaseDenom: math.LegacyOneDec(),
	})
	s.Require().NoError(s.endBlockWithoutFees())
	mended, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(2), mended.Factor)
}

// conversionFactors reads the whole table in key order.
func (s *KeeperTestSuite) conversionFactors() []types.ConversionFactor {
	var factors []types.ConversionFactor
	s.Require().NoError(s.keeper.ConversionFactors.Walk(s.ctx, nil, func(_ string, factor types.ConversionFactor) (bool, error) {
		factors = append(factors, factor)
		return false, nil
	}))
	return factors
}

// TestEndBlockerDerivesFactors pins one block's pass: the capture it reads,
// the table it leaves, the caps that table derives, and the changed-only
// refresh event.
func (s *KeeperTestSuite) TestEndBlockerDerivesFactors() {
	huge := math.LegacyNewDec(10).Power(72)
	factor := func(denom string, value math.LegacyDec, height uint64) types.ConversionFactor {
		return types.ConversionFactor{Denom: denom, Factor: value, DerivedHeight: height}
	}
	tests := []struct {
		name    string
		members []string
		// held is the table before the block.
		held  []types.ConversionFactor
		rates oracletypes.RateSet
		// capture is the block's one rate capture.
		capture []string
		// stored is the whole table after the block; absent names entries it lacks.
		stored    []types.ConversionFactor
		absent    []string
		caps      map[string]math.Int
		refreshed []types.ConversionFactor
	}{
		{
			// Taxable the block it arrives, at the unconverted reference amount, rather than
			// left exempt until its feed warms.
			name:    "an uncovered arrival seeds at one",
			members: []string{chain.XDRBaseDenom, chain.USDBaseDenom},
			rates:   oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()},
			capture: []string{chain.USDBaseDenom, chain.XDRBaseDenom},
			stored: []types.ConversionFactor{
				factor(chain.NoahBaseDenom, math.LegacyOneDec(), 1),
				factor(chain.USDBaseDenom, math.LegacyOneDec(), 1),
				factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1),
			},
			caps: map[string]math.Int{chain.USDBaseDenom: math.NewInt(1_000_000)},
			refreshed: []types.ConversionFactor{
				factor(chain.USDBaseDenom, math.LegacyOneDec(), 1),
				factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1),
				factor(chain.NoahBaseDenom, math.LegacyOneDec(), 1),
			},
		},
		{
			// A departure is not a trigger: the factor left behind keeps deriving the only cap
			// its outstanding supply can have.
			name:    "a departed member keeps its factor",
			members: []string{chain.XDRBaseDenom},
			held:    []types.ConversionFactor{factor(chain.KRWBaseDenom, math.LegacyNewDec(4), 0)},
			capture: []string{chain.XDRBaseDenom},
			stored: []types.ConversionFactor{
				factor(chain.KRWBaseDenom, math.LegacyNewDec(4), 0),
				factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1),
			},
			absent:    []string{chain.NoahBaseDenom},
			caps:      map[string]math.Int{chain.KRWBaseDenom: math.NewInt(4_000_000)},
			refreshed: []types.ConversionFactor{factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1)},
		},
		{
			name:      "the reference derives identity from a capture serving nothing",
			members:   []string{chain.XDRBaseDenom},
			capture:   []string{chain.XDRBaseDenom},
			stored:    []types.ConversionFactor{factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1)},
			absent:    []string{chain.NoahBaseDenom},
			caps:      map[string]math.Int{chain.XDRBaseDenom: math.NewInt(1_000_000)},
			refreshed: []types.ConversionFactor{factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1)},
		},
		{
			// The capture includes the reference so factors convert out of its units, yet
			// factors exist only for members.
			name:    "a non-member reference is captured but stores no factor",
			members: []string{chain.KRWBaseDenom, chain.USDBaseDenom},
			rates: oracletypes.RateSet{
				chain.KRWBaseDenom: math.LegacyNewDecWithPrec(25, 2),
				chain.USDBaseDenom: math.LegacyNewDecWithPrec(5, 1),
				chain.XDRBaseDenom: math.LegacyOneDec(),
			},
			capture: []string{chain.KRWBaseDenom, chain.USDBaseDenom, chain.XDRBaseDenom},
			stored: []types.ConversionFactor{
				factor(chain.KRWBaseDenom, math.LegacyNewDec(4), 1),
				factor(chain.NoahBaseDenom, math.LegacyOneDec(), 1),
				factor(chain.USDBaseDenom, math.LegacyNewDec(2), 1),
			},
			absent: []string{chain.XDRBaseDenom},
			caps:   map[string]math.Int{chain.KRWBaseDenom: math.NewInt(4_000_000)},
			refreshed: []types.ConversionFactor{
				factor(chain.KRWBaseDenom, math.LegacyNewDec(4), 1),
				factor(chain.USDBaseDenom, math.LegacyNewDec(2), 1),
				factor(chain.NoahBaseDenom, math.LegacyOneDec(), 1),
			},
		},
		{
			// A representable cross is kept while its unrepresentable ceiling derives uncapped.
			name:    "a lopsided factor stores and derives uncapped",
			members: []string{chain.XDRBaseDenom, chain.USDBaseDenom},
			rates: oracletypes.RateSet{
				chain.XDRBaseDenom: huge,
				chain.USDBaseDenom: math.LegacyOneDec(),
			},
			capture: []string{chain.USDBaseDenom, chain.XDRBaseDenom},
			stored: []types.ConversionFactor{
				factor(chain.NoahBaseDenom, huge, 1),
				factor(chain.USDBaseDenom, huge, 1),
				factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1),
			},
			caps: map[string]math.Int{chain.USDBaseDenom: math.ZeroInt()},
			refreshed: []types.ConversionFactor{
				factor(chain.USDBaseDenom, huge, 1),
				factor(chain.XDRBaseDenom, math.LegacyOneDec(), 1),
				factor(chain.NoahBaseDenom, huge, 1),
			},
		},
		{
			// Fees follow a live reference rate before any currency is registered.
			name:      "NOAH derives from the reference without members",
			rates:     oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)},
			capture:   []string{chain.XDRBaseDenom},
			stored:    []types.ConversionFactor{factor(chain.NoahBaseDenom, math.LegacyNewDec(2), 1)},
			refreshed: []types.ConversionFactor{factor(chain.NoahBaseDenom, math.LegacyNewDec(2), 1)},
		},
	}
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setBlockHeight(1)
			params, err := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(err)
			params.ReferenceTaxCap = math.NewInt(1_000_000)
			s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			s.setAssets(tc.members...)
			for _, held := range tc.held {
				s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, held.Denom, held))
			}
			s.setRates(tc.rates)

			s.Require().NoError(s.endBlockWithoutFees())

			s.Require().Equal([][]string{tc.capture}, s.rateCaptures)
			s.Require().Equal(tc.stored, s.conversionFactors())
			for _, denom := range tc.absent {
				_, err := s.keeper.ConversionFactors.Get(s.ctx, denom)
				s.Require().ErrorIs(err, collections.ErrNotFound)
			}
			for denom, want := range tc.caps {
				cap, err := s.keeper.GetTaxCap(s.ctx, denom)
				s.Require().NoError(err)
				s.Require().Equal(want, cap, denom)
			}
			s.requireTypedEvent(&types.EventConversionFactorsRefreshed{ConversionFactors: tc.refreshed})
		})
	}
}

// TestTaxCapDerivationBounds pins both edges of the derived read: a sub-unit
// product floors at one rather than truncating to the zero that would read
// as uncapped, and a zero reference derives the uncapped sentinel for every
// member regardless of its factor.
func (s *KeeperTestSuite) TestTaxCapDerivationBounds() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	// One base unit of reference converts to half a unit at this rate pair.
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDec(2),
	})

	s.Require().NoError(s.endBlockWithoutFees())

	usdCap, err := s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.OneInt(), usdCap)

	params.ReferenceTaxCap = math.ZeroInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	usdCap, err = s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().True(usdCap.IsZero())
}

// TestReferenceTaxCapChangeRepricesInstantly pins the derive-at-read
// semantic: a governance change to the reference amount re-prices every
// member the moment the params land, departed members included, with no
// rebuild between.
func (s *KeeperTestSuite) TestReferenceTaxCapChangeRepricesInstantly() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, types.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyNewDec(2),
	}))

	usdCap, err := s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)

	params.ReferenceTaxCap = math.NewInt(500_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	usdCap, err = s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), usdCap)
}

// TestEndBlockerDerivesNoahCrossWithoutSeeding checks NOAH receives a fee factor only from a
// usable reference rate. GetTaxCap excludes it from taxation.
func (s *KeeperTestSuite) TestEndBlockerDerivesNoahCrossWithoutSeeding() {
	s.setBlockHeight(1)
	s.setAssets(chain.USDBaseDenom)

	s.Require().NoError(s.endBlockWithoutFees())
	_, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().ErrorIs(err, collections.ErrNotFound)

	// One XDR is a quarter NOAH here, and the cross is that rate itself.
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyNewDecWithPrec(25, 2),
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(5, 1),
	})
	s.Require().NoError(s.endBlockWithoutFees())
	cross, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.25"), cross.Factor)
	// In the table, yet never tax base: the exclusion is GetTaxCap's.
	_, err = s.keeper.GetTaxCap(s.ctx, chain.NoahBaseDenom)
	s.Require().ErrorIs(err, collections.ErrNotFound)

	// A dark reference keeps the cross, like any member through an outage.
	s.setRates(oracletypes.RateSet{})
	s.Require().NoError(s.endBlockWithoutFees())
	kept, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.25"), kept.Factor)
}
