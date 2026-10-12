package keeper_test

import (
	"errors"
	"time"

	"github.com/cosmos/gogoproto/proto"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/keeper"
	"github.com/ararat-network/ark/x/treasury/types"
)

// setExposureWeights installs the committee-owned indicator weights over the
// disabled launch policy.
func (s *KeeperTestSuite) setExposureWeights(mutate func(*types.EconomicPolicy)) {
	policy, err := s.keeper.EconomicPolicy.Get(s.ctx)
	s.Require().NoError(err)
	mutate(&policy)
	s.Require().NoError(policy.Validate())
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
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
		GrossOffer:        math.ZeroInt(),
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
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)})

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
		p.VolatilitySampleSeconds = 1
	})
	s.atTime(1_000)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()

	// Three over two is a return of one half; squared, one quarter; folded at
	// half retention over a zero series, one eighth.
	s.atTime(1_001)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(3)})
	s.settleEmpty()

	state := s.getExposureState()
	s.Require().Equal(math.LegacyNewDec(3), state.LastReferencePrice)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.125"), state.VolatilityVariance)
	// The samples are a second apart; folded at half retention, one half.
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.5"), state.SampleInterval)
}

// atTime moves the suite's block time, leaving its height.
func (s *KeeperTestSuite) atTime(seconds int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockTime(time.Unix(seconds, 0))
}

func (s *KeeperTestSuite) annualisedVolatility() math.LegacyDec {
	response, err := keeper.NewQueryServerImpl(s.keeper).ExposureStatus(s.ctx, &types.QueryExposureStatusRequest{})
	s.Require().NoError(err)
	return response.AnnualisedVolatility
}

// TestExposureVolatilityPairsReturnsWithSeconds pins the per-second reading: the same return over
// six seconds annualises as variance per six seconds, so block pace does not move it.
func (s *KeeperTestSuite) TestExposureVolatilityPairsReturnsWithSeconds() {
	s.setExposureParams(func(p *types.Params) {
		p.VolatilityDecay = math.LegacyZeroDec()
		p.VolatilitySampleSeconds = 1
	})
	s.atTime(1_000)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()
	s.atTime(1_006)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(3)})
	s.settleEmpty()

	state := s.getExposureState()
	s.Require().Equal(uint64(1_006), state.LastSampleTime)
	s.Require().Equal(math.LegacyNewDec(6), state.SampleInterval)
	// A return of one half squared over six seconds, a year of seconds on top.
	expected, err := math.LegacyMustNewDecFromStr("0.25").MulInt64(365 * 86_400).QuoInt64(6).ApproxSqrt()
	s.Require().NoError(err)
	s.Require().Equal(expected, s.annualisedVolatility())
}

// TestExposureGapCountsTheTimeItSpanned pins the halt case: a return carried across a long gap is
// weighed by the seconds it spanned rather than as one block's move, so a move in step with the
// market's per-second variance leaves the reading where it was.
func (s *KeeperTestSuite) TestExposureGapCountsTheTimeItSpanned() {
	s.setExposureParams(func(p *types.Params) {
		p.VolatilityDecay = math.LegacyMustNewDecFromStr("0.5")
		p.VolatilitySampleSeconds = 1
	})
	s.atTime(1_000)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})
	s.settleEmpty()
	// One percent over six seconds.
	s.atTime(1_006)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyMustNewDecFromStr("1.01")})
	s.settleEmpty()
	before := s.annualisedVolatility()
	s.Require().True(before.IsPositive())

	// Ten percent over six hundred seconds: a hundred times the squared return over a hundred times
	// the time, the same variance per second.
	s.atTime(1_606)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyMustNewDecFromStr("1.111")})
	s.settleEmpty()
	s.Require().Equal(before, s.annualisedVolatility())
}

// TestExposureClampsReturnSample pins the bound that keeps the variance series
// inside [0, 1]: a hundredfold jump contributes exactly as much as a doubling
// would at the clamp.
func (s *KeeperTestSuite) TestExposureClampsReturnSample() {
	s.setExposureParams(func(p *types.Params) {
		p.VolatilityDecay = math.LegacyZeroDec()
		p.VolatilitySampleSeconds = 1
	})
	s.atTime(1_000)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})
	s.settleEmpty()

	s.atTime(1_001)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(100)})
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
		p.VolatilitySampleSeconds = 1
	})
	s.atTime(1_000)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()

	// The feed goes dark. Nothing is folded and the anchor holds.
	s.atTime(1_001)
	s.setRates(oracletypes.RateSet{})
	s.settleEmpty()
	s.Require().Equal(math.LegacyNewDec(2), s.getExposureState().LastReferencePrice)
	s.Require().Equal(uint64(1_000), s.getExposureState().LastSampleTime)
	s.Require().True(s.getExposureState().VolatilityVariance.IsZero())

	// It returns unchanged, so the resumed return is zero rather than a jump,
	// and spans the gap.
	s.atTime(1_002)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()
	s.Require().Equal(uint64(1_002), s.getExposureState().LastSampleTime)
	s.Require().Equal(math.LegacyNewDec(2), s.getExposureState().SampleInterval)
	s.Require().True(s.getExposureState().VolatilityVariance.IsZero())
}

// TestExposureSpacesVolatilitySamples pins volatility_sample_seconds: a block
// inside the spacing leaves the anchor where it was, and the first block past
// it folds one return spanning the whole stretch, so per-block oracle jitter
// never becomes a sample of its own.
func (s *KeeperTestSuite) TestExposureSpacesVolatilitySamples() {
	s.setExposureParams(func(p *types.Params) {
		p.VolatilityDecay = math.LegacyZeroDec()
		p.VolatilitySampleSeconds = 300
	})
	s.atTime(1_000)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})
	s.settleEmpty()

	// Six seconds on, a doubling is inside the spacing: nothing folds.
	s.atTime(1_006)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()
	state := s.getExposureState()
	s.Require().Equal(math.LegacyOneDec(), state.LastReferencePrice)
	s.Require().Equal(uint64(1_000), state.LastSampleTime)
	s.Require().True(state.VolatilityVariance.IsZero())

	// Three hundred seconds on, the return runs from the held anchor: ten
	// percent, squared, over the full span.
	s.atTime(1_300)
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyMustNewDecFromStr("1.1")})
	s.settleEmpty()
	state = s.getExposureState()
	s.Require().Equal(uint64(1_300), state.LastSampleTime)
	s.Require().Equal(math.LegacyMustNewDecFromStr("0.01"), state.VolatilityVariance)
	s.Require().Equal(math.LegacyNewDec(300), state.SampleInterval)
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
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.expectBufferBalances(0, 0)

	// Thirty redeemed against ten expanded is twenty of net pressure.
	_, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		GrossOffer:        math.NewInt(10),
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
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	// One read: no principal to place, so only the draw sizes against the
	// Buffer, and the idle block that follows returns before valuing anything.
	s.expectBufferBalances(0)

	_, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		GrossOffer:        math.ZeroInt(),
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.NewInt(40),
		RedeemedValue:     math.LegacyNewDec(40),
	})
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(20), s.getExposureState().FlowPressure)

	s.settleEmpty()
	s.Require().Equal(math.LegacyNewDec(10), s.getExposureState().FlowPressure)
}

// flowFixture prices one registry member at par so a settling block can value
// liability, and answers the Buffer reads the given blocks will make in order.
func (s *KeeperTestSuite) flowFixture(bufferReads ...int64) {
	s.setExposureParams(func(p *types.Params) {
		p.FlowDecay = math.LegacyMustNewDecFromStr("0.5")
	})
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)).AnyTimes()
	s.expectBufferBalances(bufferReads...)
}

// settleFlow runs one block that only redeems (positive) or only expands
// (negative) the given NOAH value.
func (s *KeeperTestSuite) settleFlow(value int64) {
	totals := markettypes.ConversionTotals{
		GrossOffer:        math.ZeroInt(),
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.ZeroInt(),
		RedeemedValue:     math.LegacyZeroDec(),
	}
	if value > 0 {
		totals.RedemptionOutput = math.NewInt(value)
		totals.RedeemedValue = math.LegacyNewDec(value)
	} else {
		totals.GrossOffer = math.NewInt(-value)
		totals.EligiblePrincipal = math.NewInt(-value)
	}
	_, err := s.keeper.SettleConversions(s.ctx, totals)
	s.Require().NoError(err)
}

// TestExposureFlowChurnCancels pins two-way trading as no pressure: a block
// that redeems forty and one that expands forty net to nothing. Clipping each
// block at zero before folding would have read the pair as ten.
func (s *KeeperTestSuite) TestExposureFlowChurnCancels() {
	s.flowFixture(0, 0)

	s.settleFlow(40)
	s.Require().Equal(math.LegacyNewDec(20), s.getExposureState().FlowPressure)

	// Half of twenty less half of forty is minus ten, floored at zero.
	s.settleFlow(-40)
	s.Require().True(s.getExposureState().FlowPressure.IsZero())
}

// TestExposureFlowInflowCannotHideALaterRun pins the floor: an expansion folds
// the series to zero and no further, so a redemption run that follows reads in
// full rather than first repaying credit the inflow banked.
func (s *KeeperTestSuite) TestExposureFlowInflowCannotHideALaterRun() {
	s.flowFixture(0, 0)

	s.settleFlow(-1_000)
	s.Require().True(s.getExposureState().FlowPressure.IsZero())

	s.settleFlow(40)
	s.Require().Equal(math.LegacyNewDec(20), s.getExposureState().FlowPressure)
}

// TestExposureUpdateRaisesMultiplierByStep drives one full recomputation and
// pins the step limit: the composite asks for far more than a quarter, and one
// period delivers exactly a quarter.
func (s *KeeperTestSuite) TestExposureUpdateRaisesMultiplierByStep() {
	s.setExposureWeights(func(p *types.EconomicPolicy) {
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
	s.setExposureWeights(func(p *types.EconomicPolicy) {
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
	s.setExposureWeights(func(p *types.EconomicPolicy) {
		p.LiabilityRatioWeight = math.LegacyNewDec(10)
	})
	s.setExposureParams(func(p *types.Params) {
		p.ExposureRefreshPeriodBlocks = 100
	})
	s.setBlockHeight(50)

	// No supply or balance expectations: reaching the fold would fail here.
	s.Require().NoError(s.beginBlock())

	s.Require().Equal(math.LegacyOneDec(), s.getExposureState().Multiplier)
}

// expectRefreshSupply stubs a refresh's reads over a USD-only registry: the
// member's supply, NOAH supply, and empty protocol funds.
func (s *KeeperTestSuite) expectRefreshSupply(usd, noah math.Int) {
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewCoin(chain.USDBaseDenom, usd)).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewCoin(chain.NoahBaseDenom, noah)).AnyTimes()
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
}

// exposureRefreshed decodes the block's last refresh event.
func (s *KeeperTestSuite) exposureRefreshed() *types.EventExposureRefreshed {
	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != proto.MessageName(&types.EventExposureRefreshed{}) {
			continue
		}
		parsed, err := sdk.ParseTypedEvent(sdk.Events{events[i]}.ToABCIEvents()[0])
		s.Require().NoError(err)
		event, ok := parsed.(*types.EventExposureRefreshed)
		s.Require().True(ok)
		return event
	}
	s.FailNow("exposure refresh event not found")
	return nil
}

// TestExposureRefreshBoundsTheMultiplier pins every bound between the composed
// target and the stored multiplier: the step either way, the governance cap,
// saturation, and the floor.
func (s *KeeperTestSuite) TestExposureRefreshBoundsTheMultiplier() {
	maxWeight := types.MaxExposureWeight
	tests := []struct {
		name    string
		weights func(*types.EconomicPolicy)
		params  func(*types.Params)
		// state writes the stored series straight to the store, unvalidated.
		state      func(*types.ExposureState)
		usd, noah  math.Int
		previous   string
		uncapped   string
		multiplier string
	}{
		{
			// A ratio of one half at a weight of ten composes to six.
			name:       "an upward target past one step moves one step",
			weights:    func(p *types.EconomicPolicy) { p.LiabilityRatioWeight = math.LegacyNewDec(10) },
			previous:   "1",
			uncapped:   "6",
			multiplier: "1.25",
		},
		{
			name:       "an upward target within one step lands on it",
			weights:    func(p *types.EconomicPolicy) { p.LiabilityRatioWeight = math.LegacyMustNewDecFromStr("0.2") },
			previous:   "1",
			uncapped:   "1.1",
			multiplier: "1.1",
		},
		{
			name:       "a downward target past one step moves one step",
			state:      func(st *types.ExposureState) { st.Multiplier = math.LegacyNewDec(2) },
			previous:   "2",
			uncapped:   "1",
			multiplier: "1.75",
		},
		{
			name:       "a downward target within one step lands on it",
			weights:    func(p *types.EconomicPolicy) { p.LiabilityRatioWeight = math.LegacyMustNewDecFromStr("0.2") },
			state:      func(st *types.ExposureState) { st.Multiplier = math.LegacyMustNewDecFromStr("1.2") },
			previous:   "1.2",
			uncapped:   "1.1",
			multiplier: "1.1",
		},
		{
			// A whole step of one would reach two; the cap holds it at one and a half.
			name:    "a target above the cap lands on the cap",
			weights: func(p *types.EconomicPolicy) { p.LiabilityRatioWeight = math.LegacyNewDec(10) },
			params: func(p *types.Params) {
				p.MultiplierCap = math.LegacyMustNewDecFromStr("1.5")
				p.MultiplierMaxStep = math.LegacyOneDec()
			},
			previous:   "1",
			uncapped:   "6",
			multiplier: "1.5",
		},
		{
			// The step down reaches 2.75, still above the cap governance lowered to two.
			name:       "a lowered cap clamps a stored multiplier",
			params:     func(p *types.Params) { p.MultiplierCap = math.LegacyNewDec(2) },
			state:      func(st *types.ExposureState) { st.Multiplier = math.LegacyNewDec(3) },
			previous:   "3",
			uncapped:   "1",
			multiplier: "2",
		},
		{
			// A ratio of 10^72 at the weight ceiling leaves the Dec domain.
			name:       "an unrepresentable surcharge saturates at the cap",
			weights:    func(p *types.EconomicPolicy) { p.LiabilityRatioWeight = maxWeight },
			usd:        math.NewIntWithDecimal(1, 72),
			noah:       math.OneInt(),
			previous:   "1",
			uncapped:   "4",
			multiplier: "1.25",
		},
		{
			// The surcharge is the largest representable integer, so adding one overflows.
			name:       "an unrepresentable factor saturates at the cap",
			weights:    func(p *types.EconomicPolicy) { p.LiabilityRatioWeight = math.LegacyOneDec() },
			usd:        maxRepresentableInt(),
			noah:       math.OneInt(),
			previous:   "1",
			uncapped:   "4",
			multiplier: "1.25",
		},
		{
			// A liability factor of 10^76 fits; the full-variance volatility factor on top does not.
			name: "an unrepresentable product saturates at the cap",
			weights: func(p *types.EconomicPolicy) {
				p.LiabilityRatioWeight = maxWeight
				p.VolatilityWeight = maxWeight
			},
			state: func(st *types.ExposureState) {
				st.VolatilityVariance = math.LegacyOneDec()
				st.SampleInterval = math.LegacyOneDec()
			},
			usd:        math.NewIntWithDecimal(1, 70),
			noah:       math.OneInt(),
			previous:   "1",
			uncapped:   "4",
			multiplier: "1.25",
		},
		{
			// Backstop: validation refuses a stored multiplier below one.
			name:       "a stored multiplier below one restarts from one",
			state:      func(st *types.ExposureState) { st.Multiplier = math.LegacyMustNewDecFromStr("0.5") },
			previous:   "1",
			uncapped:   "1",
			multiplier: "1",
		},
		{
			// Backstop: non-negative weights, liability, and flow keep every target at one or
			// above; a negative flow series, which validation refuses, composes to one half.
			name:       "a target below one clamps to one",
			weights:    func(p *types.EconomicPolicy) { p.FlowWeight = math.LegacyOneDec() },
			state:      func(st *types.ExposureState) { st.FlowPressure = math.LegacyNewDec(-50) },
			previous:   "1",
			uncapped:   "0.5",
			multiplier: "1",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			if tc.weights != nil {
				s.setExposureWeights(tc.weights)
			}
			s.setExposureParams(func(p *types.Params) {
				p.ExposureRefreshPeriodBlocks = 10
				if tc.params != nil {
					tc.params(p)
				}
			})
			if tc.state != nil {
				state := types.DefaultExposureState()
				tc.state(&state)
				s.Require().NoError(s.keeper.ExposureState.Set(s.ctx, state))
			}
			usd, noah := math.NewInt(100), math.NewInt(200)
			if !tc.usd.IsNil() {
				usd, noah = tc.usd, tc.noah
			}
			s.setAssets(chain.USDBaseDenom)
			s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
			s.expectRefreshSupply(usd, noah)
			s.setBlockHeight(9)

			s.Require().NoError(s.beginBlock())

			multiplier := math.LegacyMustNewDecFromStr(tc.multiplier)
			s.Require().True(multiplier.Equal(s.getExposureState().Multiplier),
				"stored multiplier %s", s.getExposureState().Multiplier)
			event := s.exposureRefreshed()
			s.Require().True(math.LegacyMustNewDecFromStr(tc.previous).Equal(event.PreviousMultiplier),
				"previous multiplier %s", event.PreviousMultiplier)
			s.Require().True(math.LegacyMustNewDecFromStr(tc.uncapped).Equal(event.UncappedMultiplier),
				"uncapped multiplier %s", event.UncappedMultiplier)
			s.Require().True(multiplier.Equal(event.Multiplier), "multiplier %s", event.Multiplier)
		})
	}
}

// TestExposureRefreshRetriesUnavailableInputs pins the owed flag as the retry:
// a rate the block cannot supply holds the refresh for the next block, which
// settles it off cadence, while any other fault fails the block.
func (s *KeeperTestSuite) TestExposureRefreshRetriesUnavailableInputs() {
	tests := []struct {
		name string
		// fold fails the registry-wide pricing fold.
		fold error
		// rate, when set, values the largest supply past the Dec domain.
		rate  math.LegacyDec
		holds bool
	}{
		{
			name:  "stale rate",
			fold:  errorsmod.Wrap(oracletypes.ErrStaleExchangeRate, chain.USDBaseDenom),
			holds: true,
		},
		{
			name:  "unknown denomination",
			fold:  errorsmod.Wrap(oracletypes.ErrUnknownDenom, chain.USDBaseDenom),
			holds: true,
		},
		{
			name:  "unrepresentable valuation",
			rate:  math.LegacyNewDec(2),
			holds: true,
		},
		{
			name: "store fault",
			fold: errors.New("registry store failure"),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.setExposureWeights(func(p *types.EconomicPolicy) {
				p.LiabilityRatioWeight = math.LegacyNewDec(10)
			})
			s.setExposureParams(func(p *types.Params) {
				p.ExposureRefreshPeriodBlocks = 10
			})
			s.setAssets(chain.USDBaseDenom)
			supply := math.NewInt(100)
			rate := math.LegacyOneDec()
			if !tc.rate.IsNil() {
				supply, rate = maxRepresentableInt(), tc.rate
			}
			s.setRates(oracletypes.RateSet{chain.USDBaseDenom: rate})
			s.expectRefreshSupply(supply, math.NewInt(200))
			s.pricedAssetsErr = tc.fold
			s.setBlockHeight(9)

			err := s.beginBlock()
			if !tc.holds {
				s.Require().ErrorIs(err, tc.fold)
				return
			}
			s.Require().NoError(err)
			s.requireNoTypedEvent(&types.EventExposureRefreshed{})
			s.Require().Equal(math.LegacyOneDec(), s.getExposureState().Multiplier)
			pending, err := s.keeper.ExposureRefreshPending.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(pending)

			// The input returns, and the next block settles what was owed.
			s.pricedAssetsErr = nil
			s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
			s.setBlockHeight(10)
			s.Require().NoError(s.beginBlock())
			s.Require().Equal(math.LegacyMustNewDecFromStr("1.25"), s.getExposureState().Multiplier)
			s.Require().Equal(uint64(10), s.getExposureState().LastRefreshHeight)
			pending, err = s.keeper.ExposureRefreshPending.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().False(pending)
		})
	}
}

// TestExposureRescalesAnchorAcrossReferenceMove pins the unit change as a unit
// change: without the rescale the next block would read a new-unit price
// against an old-unit anchor and record the cross rate as a market move.
func (s *KeeperTestSuite) TestExposureRescalesAnchorAcrossReferenceMove() {
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()
	s.Require().Equal(math.LegacyNewDec(2), s.getExposureState().LastReferencePrice)

	// One USD is two XDR, so an anchor of two NOAH per XDR is four NOAH per USD
	// — the anchor is a price, and moves by the reciprocal of what a quantity
	// of XDR would.
	s.Require().NoError(s.keeper.RebaseReference(
		s.ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracletypes.RateSet{
			chain.XDRBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom: math.LegacyNewDec(2),
		},
	))

	state := s.getExposureState()
	s.Require().Equal(math.LegacyNewDec(4), state.LastReferencePrice)
	// The dimensionless series are untouched by a change of unit.
	s.Require().Equal(math.LegacyOneDec(), state.Multiplier)
	s.Require().True(state.VolatilityVariance.IsZero())
}

// TestExposureRebasedAnchorRecordsNoReturnWithoutAMove checks a correctly rebased anchor meets the
// new unit's live price without recording a spurious market return.
func (s *KeeperTestSuite) TestExposureRebasedAnchorRecordsNoReturnWithoutAMove() {
	s.setRates(oracletypes.RateSet{chain.XDRBaseDenom: math.LegacyOneDec()})
	s.settleEmpty()
	s.Require().Equal(math.LegacyOneDec(), s.getExposureState().LastReferencePrice)

	// One USD is two NOAH and one XDR is one, so an anchor of one NOAH per
	// XDR is two NOAH per USD.
	s.Require().NoError(s.keeper.RebaseReference(
		s.ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracletypes.RateSet{
			chain.XDRBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom: math.LegacyNewDec(2),
		},
	))
	s.reference = chain.USDBaseDenom
	s.Require().Equal(math.LegacyNewDec(2), s.getExposureState().LastReferencePrice)

	// The market has not moved: the live NOAH price of a USD is the two the
	// anchor now states.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyNewDec(2)})
	s.settleEmpty()
	s.Require().True(s.getExposureState().VolatilityVariance.IsZero())
}
