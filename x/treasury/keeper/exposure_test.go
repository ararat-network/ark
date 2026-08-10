package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// setExposureWeights installs the committee-owned indicator weights over the
// disabled launch policy.
func (s *KeeperTestSuite) setExposureWeights(mutate func(*types.MonetaryPolicy)) {
	policy, err := s.keeper.MonetaryPolicy.Get(s.ctx)
	s.Require().NoError(err)
	mutate(&policy)
	s.Require().NoError(policy.Validate())
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
}

// setExposureParams installs the governance-owned machinery over the launch
// defaults, leaving every field a test does not name at its default.
func (s *KeeperTestSuite) setExposureParams(mutate func(*types.Params)) {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	exposure := params
	mutate(&exposure)
	params = exposure
	s.Require().NoError(params.Validate())
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
}

// getExposureState reads the stored state, answering the default when nothing
// has written one — which is what a block that sampled nothing and applied
// nothing leaves behind, and is the same fallback the keeper serves its own
// readers.
func (s *KeeperTestSuite) getExposureState() types.ExposureState {
	state, err := s.keeper.ExposureState.Get(s.ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return types.DefaultExposureState()
	}
	s.Require().NoError(err)
	return state
}

// settleEmpty runs a block that converted nothing, which is the cheapest way to
// drive one round of sampling.
func (s *KeeperTestSuite) settleEmpty() {
	burn, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.ZeroInt(),
		RedeemedValue:     math.LegacyZeroDec(),
	})
	s.Require().NoError(err)
	s.Require().True(burn.IsZero())
}

// TestExposureFirstSampleRecordsPriceWithoutReturn pins the anchor rule: a
// price with nothing to compare against is not a zero return, which would
// record calm the chain never observed.
func (s *KeeperTestSuite) TestExposureFirstSampleRecordsPriceWithoutReturn() {
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyNewDec(2)})

	s.settleEmpty()

	state := s.getExposureState()
	s.Require().Equal(math.LegacyNewDec(2), state.LastReferencePrice)
	s.Require().True(state.VolatilityVariance.IsZero())
}

// TestExposureFoldsSquaredReturnIntoVariance checks the EWMA against a hand
// computation: a decay of one half over a return of one half contributes a
// quarter of the squared return.
func (s *KeeperTestSuite) TestExposureFoldsSquaredReturnIntoVariance() {
	s.setExposureParams(func(p *types.Params) {
		p.VolatilityDecay = math.LegacyMustNewDecFromStr("0.5")
	})
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()

	// Three over two is a return of one half; squared, one quarter; folded at
	// half retention over a zero series, one eighth.
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyNewDec(3)})
	s.settleEmpty()

	state := s.getExposureState()
	s.Require().Equal(math.LegacyNewDec(3), state.LastReferencePrice)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.125"), state.VolatilityVariance)
}

// TestExposureClampsReturnSample pins the bound that keeps the variance series
// inside [0, 1]: a hundredfold jump contributes exactly as much as a doubling
// would at the clamp.
func (s *KeeperTestSuite) TestExposureClampsReturnSample() {
	s.setExposureParams(func(p *types.Params) {
		p.VolatilityDecay = math.LegacyZeroDec()
	})
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyOneDec()})
	s.settleEmpty()

	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyNewDec(100)})
	s.settleEmpty()

	// A zero decay makes the series the latest sample alone, so the clamp is
	// visible directly: one, not ninety-nine squared.
	s.Require().Equal(math.LegacyOneDec(), s.getExposureState().VolatilityVariance)
}

// TestExposureSkipsSampleWithoutReferenceRate pins absent evidence as absent
// rather than calm. The anchor survives the gap, so the return resumes across
// it and measures the move that actually happened.
func (s *KeeperTestSuite) TestExposureSkipsSampleWithoutReferenceRate() {
	s.setExposureParams(func(p *types.Params) {
		p.VolatilityDecay = math.LegacyZeroDec()
	})
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()

	// The feed goes dark. Nothing is folded and the anchor holds.
	s.setRates(oracletypes.RateSet{})
	s.settleEmpty()
	s.Require().Equal(math.LegacyNewDec(2), s.getExposureState().LastReferencePrice)
	s.Require().True(s.getExposureState().VolatilityVariance.IsZero())

	// It returns unchanged, so the resumed return is zero rather than a jump.
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()
	s.Require().True(s.getExposureState().VolatilityVariance.IsZero())
}

// TestExposureFoldsNetRedemptionFlow pins the flow sample as the redemption
// excess: a block whose expansions matched its redemptions exerted no
// one-directional pressure.
func (s *KeeperTestSuite) TestExposureFoldsNetRedemptionFlow() {
	s.setExposureParams(func(p *types.Params) {
		p.FlowDecay = math.LegacyZeroDec()
	})
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.expectBufferBalances(0, 0)

	// Thirty redeemed against ten expanded is twenty of net pressure.
	_, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		EligiblePrincipal: math.NewInt(10),
		RedemptionOutput:  math.NewInt(30),
		RedeemedValue:     math.LegacyNewDec(30),
	})
	s.Require().NoError(err)

	s.Require().Equal(math.LegacyNewDec(20), s.getExposureState().FlowPressure)
}

// TestExposureIdleBlockDecaysFlow pins the series as a function of elapsed time
// rather than elapsed activity: a quiet stretch cools the estimate instead of
// freezing it at whatever the last converting block saw.
func (s *KeeperTestSuite) TestExposureIdleBlockDecaysFlow() {
	s.setExposureParams(func(p *types.Params) {
		p.FlowDecay = math.LegacyMustNewDecFromStr("0.5")
	})
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	// One read: no principal to place, so only the draw sizes against the
	// Buffer, and the idle block that follows returns before valuing anything.
	s.expectBufferBalances(0)

	_, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.NewInt(40),
		RedeemedValue:     math.LegacyNewDec(40),
	})
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(20), s.getExposureState().FlowPressure)

	s.settleEmpty()
	s.Require().Equal(math.LegacyNewDec(10), s.getExposureState().FlowPressure)
}

// TestExposureUpdateRaisesMultiplierByStep drives one full recomputation and
// pins the step limit: the composite asks for far more than a quarter, and one
// period delivers exactly a quarter.
func (s *KeeperTestSuite) TestExposureUpdateRaisesMultiplierByStep() {
	s.setExposureWeights(func(p *types.MonetaryPolicy) {
		p.LiabilityRatioWeight = math.LegacyNewDec(10)
	})
	s.setExposureParams(func(p *types.Params) {
		p.ExposureRefreshPeriodBlocks = 10
	})
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 200)).AnyTimes()
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
	s.setBlockHeight(9)
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.beginBlock())

	// A ratio of one half at a weight of ten composes to six, which the step
	// caps at one and a quarter.
	state := s.getExposureState()
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.5"), state.LiabilityRatio)
	s.Require().True(state.FlowRatio.IsZero())
	s.Require().Equal(math.LegacyMustNewDecFromStr("1.25"), state.Multiplier)
	// Stamped by the refresh, not by the sampling every block runs.
	s.Require().Equal(uint64(9), state.LastRefreshHeight)
	s.requireTypedEvent(&types.EventExposureRefreshed{
		PreviousMultiplier:   math.LegacyOneDec(),
		Multiplier:           math.LegacyMustNewDecFromStr("1.25"),
		UncappedMultiplier:   math.LegacyNewDec(6),
		LiabilityRatio:       math.LegacyMustNewDecFromStr("0.5"),
		AnnualisedVolatility: math.LegacyZeroDec(),
		FlowRatio:            math.LegacyZeroDec(),
	})
}

// TestExposureUpdateHoldsWithoutCirculatingSupply pins the missing denominator
// as a hold rather than a reading: a chain whose NOAH is entirely protocol-held
// has no market capitalisation to measure leverage against.
func (s *KeeperTestSuite) TestExposureUpdateHoldsWithoutCirculatingSupply() {
	s.setExposureWeights(func(p *types.MonetaryPolicy) {
		p.LiabilityRatioWeight = math.LegacyNewDec(10)
	})
	s.setExposureParams(func(p *types.Params) {
		p.ExposureRefreshPeriodBlocks = 10
	})
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
	s.setBlockHeight(9)
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.beginBlock())

	s.Require().Equal(math.LegacyOneDec(), s.getExposureState().Multiplier)
	// The period is owed rather than forgiven, so the next block asks again.
	pending, err := s.keeper.ExposureRefreshPending.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(pending)
}

// TestExposureUpdateSkippedOnOffCadenceBlock pins the cadence: an ordinary
// block reads the owed flag and nothing else, which is what keeps the registry
// fold off the per-block path.
func (s *KeeperTestSuite) TestExposureUpdateSkippedOnOffCadenceBlock() {
	s.setExposureWeights(func(p *types.MonetaryPolicy) {
		p.LiabilityRatioWeight = math.LegacyNewDec(10)
	})
	s.setExposureParams(func(p *types.Params) {
		p.ExposureRefreshPeriodBlocks = 100
	})
	s.setBlockHeight(50)
	s.expectValidatorFees(sdk.NewCoins())

	// No supply or balance expectations: reaching the fold would fail here.
	s.Require().NoError(s.beginBlock())

	s.Require().Equal(math.LegacyOneDec(), s.getExposureState().Multiplier)
}

// TestExposureRescalesAnchorAcrossReferenceMove pins the unit change as a unit
// change: without the rescale the next block would read a new-unit price
// against an old-unit anchor and record the cross rate as a market move.
func (s *KeeperTestSuite) TestExposureRescalesAnchorAcrossReferenceMove() {
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()
	s.Require().Equal(math.LegacyNewDec(2), s.getExposureState().LastReferencePrice)

	// One SDR is two USD, so an anchor of two SDR per NOAH is four USD per NOAH.
	s.Require().NoError(s.keeper.RebaseTaxCap(
		s.ctx,
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
		oracletypes.RateSet{
			chain.SDRBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom: math.LegacyNewDec(2),
		},
	))

	state := s.getExposureState()
	s.Require().Equal(math.LegacyNewDec(4), state.LastReferencePrice)
	// The dimensionless series are untouched by a change of unit.
	s.Require().Equal(math.LegacyOneDec(), state.Multiplier)
	s.Require().True(state.VolatilityVariance.IsZero())
}
