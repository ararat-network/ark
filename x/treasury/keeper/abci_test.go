package keeper_test

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// TestEndBlockerAccruesRewardFundingFromTheFirstBlock pins the accrual's
// phase: at EndBlock the collector holds the block's own fees, so the first
// real block already accrues — there is no genesis-height skip to inherit
// from the BeginBlock era, whose read had no previous block to value.
func (s *KeeperTestSuite) TestEndBlockerAccruesRewardFundingFromTheFirstBlock() {
	s.setBlockHeight(1)
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.endBlock())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
}

// TestBeginBlockerValuesNoLiability pins the scan that left this hook. The
// aggregate has exactly one consumer that runs every block — conversion
// settlement — and it runs at the end of one, where the figure can be built
// from final state. So BeginBlock values nothing, an idle block folds the
// registry not at all, and arithmetic that used to fail the block while priming
// can no longer reach it.
//
// The registry holds supply that overflowed the conversion under the old
// priming, and no supply read is stubbed at all: the mock fails the test on any
// call, which is what asserts the fold is gone rather than merely quiet.
func (s *KeeperTestSuite) TestBeginBlockerValuesNoLiability() {
	s.setBlockHeight(2)
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(1, 18),
	})

	s.Require().NoError(s.beginBlock())
	s.requireNoTypedEvent(&types.EventLiabilityIncomplete{})
}

func (s *KeeperTestSuite) TestEndBlockerAccruesRewardFunding() {
	s.setBlockHeight(2)
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.endBlock())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
}

func (s *KeeperTestSuite) TestEndBlockerValuesStableFeesAgainstOraclePricedMembership() {
	s.setBlockHeight(2)
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 5)))
	// axdr is oracle-priced by the suite defaults, so the fee is valued; the
	// membership set that admits it comes from the asset registry, not from
	// any Treasury- or Oracle-owned list.
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})

	s.Require().NoError(s.endBlock())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(5), funding.ValidatorFeeValue)
}

// TestBeginBlockerDerivesFactorsEveryBlock pins the per-block contract: a
// servable member's factor tracks this block's rate, the derived cap tracks
// the factor, and the changed-only event stays quiet on a block that
// re-derives every factor to the value it already held.
func (s *KeeperTestSuite) TestBeginBlockerDerivesFactorsEveryBlock() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDec(2),
	})

	s.Require().NoError(s.beginBlock())

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
	s.Require().NoError(s.beginBlock())
	s.requireNoTypedEvent(&types.EventConversionFactorsRefreshed{})
	usd, err := s.keeper.ConversionFactors.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(uint64(1), usd.DerivedHeight)

	// A moved rate re-derives the one factor it moved.
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDec(3),
	})
	s.Require().NoError(s.beginBlock())
	usdCap, err = s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(3_000_000), usdCap)
	s.requireTypedEvent(&types.EventConversionFactorsRefreshed{ConversionFactors: []types.ConversionFactor{
		{Denom: chain.USDBaseDenom, Factor: math.LegacyNewDec(3), DerivedHeight: 1},
	}})
}

// TestBeginBlockerKeepsFactorThroughOutage proves a dark feed keeps its last
// factor, the derived cap keeps clamping through the outage, and the pass
// mends the factor the block the feed returns — every block is the retry.
func (s *KeeperTestSuite) TestBeginBlockerKeepsFactorThroughOutage() {
	s.setBlockHeight(1)
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.KRWBaseDenom, chain.XDRBaseDenom)
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, types.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: math.LegacyMustNewDecFromStr("0.000007"),
	}))

	// The outage block: no rate serves akrw, so its factor is kept untouched.
	s.Require().NoError(s.beginBlock())
	kept, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.000007"), kept.Factor)
	s.Require().Equal(uint64(0), kept.DerivedHeight)

	// Every member taxes through the outage: the kept factor keeps deriving
	// the cap that clamps — 10 would be owed at the rate, the derived cap of
	// 7 binds. An oracle outage must not make a healthy denomination
	// untransactable — nor tax-exempt.
	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 7)), tax)

	// The feed returns and the very next block re-derives.
	s.setRates(oracletypes.RateSet{
		chain.KRWBaseDenom: math.LegacyNewDec(2),
		chain.XDRBaseDenom: math.LegacyOneDec(),
	})
	s.Require().NoError(s.beginBlock())
	mended, err := s.keeper.ConversionFactors.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(2), mended.Factor)
}

// TestBeginBlockerSeedsUncoveredArrivalAtOne pins the arrival verdict of a
// partial pass: a member holding no factor whose rate cannot serve is seeded
// at one — taxable the block it arrives, at the unconverted reference amount
// — rather than left exempt until its feed warms.
func (s *KeeperTestSuite) TestBeginBlockerSeedsUncoveredArrivalAtOne() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})

	s.Require().NoError(s.beginBlock())

	usdCap, err := s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), usdCap)
	s.requireTypedEvent(&types.EventConversionFactorsRefreshed{ConversionFactors: []types.ConversionFactor{
		{Denom: chain.USDBaseDenom, Factor: math.LegacyOneDec(), DerivedHeight: 1},
		{Denom: chain.XDRBaseDenom, Factor: math.LegacyOneDec(), DerivedHeight: 1},
		{Denom: chain.NoahBaseDenom, Factor: math.LegacyOneDec(), DerivedHeight: 1},
	}})
}

// TestBeginBlockerLeavesDepartedFactorUntouched proves a departure is not a
// trigger: the factor a member leaves behind keeps deriving the only cap its
// outstanding supply can have.
func (s *KeeperTestSuite) TestBeginBlockerLeavesDepartedFactorUntouched() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.XDRBaseDenom)
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, types.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: math.LegacyNewDec(4),
	}))

	s.Require().NoError(s.beginBlock())

	krwCap, err := s.keeper.GetTaxCap(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(4_000_000), krwCap)
}

// TestBeginBlockerDerivesReferenceIdentityWithoutServedRates pins the identity
// arm: a membership of the reference alone derives factor one from a capture
// that serves nothing.
func (s *KeeperTestSuite) TestBeginBlockerDerivesReferenceIdentityWithoutServedRates() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.XDRBaseDenom)

	s.Require().NoError(s.beginBlock())

	xdrCap, err := s.keeper.GetTaxCap(s.ctx, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), xdrCap)
	s.Require().Equal([][]string{{chain.XDRBaseDenom}}, s.rateCaptures)
}

// TestBeginBlockerCapturesReferenceWhenNotAMember pins the capture shape:
// membership excludes the reference, yet the capture must include it so the
// factors can convert out of reference units, and no factor is stored for it
// — factors exist only for members.
func (s *KeeperTestSuite) TestBeginBlockerCapturesReferenceWhenNotAMember() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.KRWBaseDenom, chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.KRWBaseDenom: math.LegacyNewDec(4),
		chain.USDBaseDenom: math.LegacyNewDec(2),
		chain.XDRBaseDenom: math.LegacyOneDec(),
	})

	s.Require().NoError(s.beginBlock())

	s.Require().NotEmpty(s.rateCaptures)
	s.Require().Equal(
		[]string{chain.KRWBaseDenom, chain.USDBaseDenom, chain.XDRBaseDenom},
		s.rateCaptures[len(s.rateCaptures)-1],
	)
	krwCap, err := s.keeper.GetTaxCap(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(4_000_000), krwCap)
	_, err = s.keeper.ConversionFactors.Get(s.ctx, chain.XDRBaseDenom)
	s.Require().Error(err)
}

// TestBeginBlockerStoresLopsidedFactorDerivingUncapped pins the degrade for a
// hyper-lopsided rate: the factor itself is representable and stored, and the
// derived cap — whose true value exceeds the decimal domain — reads as the
// uncapped sentinel. A ceiling too large to express is no ceiling; the old
// seed-at-reference degrade imposed one many orders of magnitude tighter than
// policy asked for.
func (s *KeeperTestSuite) TestBeginBlockerStoresLopsidedFactorDerivingUncapped() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.XDRBaseDenom, chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDec(10).Power(72),
	})

	s.Require().NoError(s.beginBlock())

	stored, err := s.keeper.ConversionFactors.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(10).Power(72), stored.Factor)
	usdCap, err := s.keeper.GetTaxCap(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().True(usdCap.IsZero())
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
		chain.XDRBaseDenom: math.LegacyNewDec(2),
		chain.USDBaseDenom: math.LegacyOneDec(),
	})

	s.Require().NoError(s.beginBlock())

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

// TestBeginBlockerDerivesNoahCrossWithoutSeeding pins the numeraire's
// difference from the members: no servable reference rate means no entry —
// NOAH is refused as a fee denom rather than seeded, because nothing forces
// gas to be paid in it — and the first real rate creates the entry in the
// table, excluded from the tax base by GetTaxCap alone.
func (s *KeeperTestSuite) TestBeginBlockerDerivesNoahCrossWithoutSeeding() {
	s.setBlockHeight(1)
	s.setAssets(chain.USDBaseDenom)

	s.Require().NoError(s.beginBlock())
	_, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().ErrorIs(err, collections.ErrNotFound)

	// One NOAH is four XDR here, so one reference unit is a quarter NOAH.
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyNewDec(4),
		chain.USDBaseDenom: math.LegacyNewDec(2),
	})
	s.Require().NoError(s.beginBlock())
	cross, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.25"), cross.Factor)
	// In the table, yet never tax base: the exclusion is GetTaxCap's.
	_, err = s.keeper.GetTaxCap(s.ctx, chain.NoahBaseDenom)
	s.Require().ErrorIs(err, collections.ErrNotFound)

	// A dark reference keeps the cross, like any member through an outage.
	s.setRates(oracletypes.RateSet{})
	s.Require().NoError(s.beginBlock())
	kept, err := s.keeper.ConversionFactors.Get(s.ctx, chain.NoahBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.25"), kept.Factor)
}
