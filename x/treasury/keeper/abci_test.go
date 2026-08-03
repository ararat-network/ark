package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestBeginBlockerSkipsRewardFundingAtGenesisHeight() {
	s.setBlockHeight(1)

	s.Require().NoError(s.beginBlock())
	s.requireDefaultRewardFunding()
}

// TestBeginBlockerPrimesLiabilitySnapshot pins the valuation this module now
// owns. It moved out of the ABCI preblocker, so BeginBlocker is the sole place
// the block's claimable aggregate is established, and transaction-time callers
// depend on finding it already there rather than rescanning the registry.
func (s *KeeperTestSuite) TestBeginBlockerPrimesLiabilitySnapshot() {
	s.setBlockHeight(2)
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	})
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.beginBlock())
	s.requireLiabilitySnapshot(math.LegacyNewDec(200), true)
}

// TestBeginBlockerPrimesLiabilityAtGenesisHeight pins priming outside the
// height gate that gets reward funding. The preblocker primed unconditionally,
// and the first block values liability exactly as every later one does, so a
// transaction in it must not be the one call that pays for a registry scan.
func (s *KeeperTestSuite) TestBeginBlockerPrimesLiabilityAtGenesisHeight() {
	s.setBlockHeight(1)
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 50)).Times(1)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})

	s.Require().NoError(s.beginBlock())
	s.requireLiabilitySnapshot(math.LegacyNewDec(50), true)
	s.requireDefaultRewardFunding()
}

// TestBeginBlockerFailsBlockWhenPrimingFails keeps priming's failure semantics
// across the move: valuation arithmetic leaving the supported domain halts the
// block rather than degrading, and it does so before any later BeginBlocker
// step observes state built on it.
func (s *KeeperTestSuite) TestBeginBlockerFailsBlockWhenPrimingFails() {
	s.setBlockHeight(2)
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewCoin(chain.USDBaseDenom, math.NewIntWithDecimal(1, 60))).Times(1)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(1, 18),
	})

	err := s.beginBlock()
	s.Require().ErrorContains(err, "priming liability snapshot")
	s.Require().ErrorIs(err, oracletypes.ErrConversionOutOfRange)
}

func (s *KeeperTestSuite) TestBeginBlockerAccruesRewardFunding() {
	s.setBlockHeight(2)
	s.expectValidatorFees(sdk.NewCoins())

	// The default reference cap is zero, so the refresh is rate-free and this
	// block is observably pure reward-funding accrual.
	s.Require().NoError(s.beginBlock())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
}

func (s *KeeperTestSuite) TestBeginBlockerValuesStableFeesAgainstOraclePricedMembership() {
	s.setBlockHeight(2)
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 5)))
	// asdr is oracle-priced by the suite defaults, so the fee is valued; the
	// membership set that admits it comes from the asset registry, not from
	// any Treasury- or Oracle-owned list.
	s.setRates(oracletypes.RateSet{chain.SDRBaseDenom: math.LegacyOneDec()})

	s.Require().NoError(s.beginBlock())
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(5), funding.ValidatorFeeValue)
}

// TestBeginBlockerRebuildsDriftedCapsMidPeriod pins the membership trigger:
// the caps are compared against the registry itself, so a member left
// uncovered is served on the next block rather than waiting for a cadence
// boundary. Nothing has to announce the move for this to fire.
func (s *KeeperTestSuite) TestBeginBlockerRebuildsDriftedCapsMidPeriod() {
	s.setBlockHeight(1)
	s.setAssets(chain.SDRBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(5)))

	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// The lone member is the reference itself, so the rebuild needs no rates:
	// the strict oracle mock stays unprogrammed and the stored caps are the
	// whole observable effect.
	s.Require().NoError(s.beginBlock())

	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), sdrCap)
	// The non-member's cap is untouched by the rebuild rather than swept with
	// it: it is the only cap that denomination can have.
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(5), usdCap)
}

func (s *KeeperTestSuite) TestBeginBlockerRebuildsCapsOnMembershipChange() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// akrw is outside the membership set while its seeded cap stays behind:
	// the rebuild must derive the members without disturbing it.
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.KRWBaseDenom, math.NewInt(7)))
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDec(2),
	}, nil)

	s.Require().NoError(s.beginBlock())

	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), sdrCap)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)
	krwCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(7), krwCap)
	// The rebuild leaves the cadence flag down, and the caps now cover
	// membership, so the next block has nothing to do. The event reports the
	// denoms this rebuild derived, not the kept cap it left alone.
	s.requireTaxCapRefreshPending(false)
	s.requireTypedEvent(&types.EventTaxCapsUpdated{TaxCaps: []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.NewInt(1_000_000)},
		{Denom: chain.USDBaseDenom, TaxCap: math.NewInt(2_000_000)},
	}})
}

// TestBeginBlockerKeepsCapsWhenMembershipShrinks proves a departure is not a
// rebuild trigger. The strict oracle mock is left unprogrammed, so any attempt
// to re-derive would fail the test: coverage is containment of the members,
// and losing one leaves every survivor already covered.
func (s *KeeperTestSuite) TestBeginBlockerKeepsCapsWhenMembershipShrinks() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.SDRBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(1_000_000)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(2_000_000)))
	// The suite leaves the cadence flag down and this height closes no period,
	// so coverage is the only trigger under test.

	s.Require().NoError(s.beginBlock())

	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)
	s.requireTaxCapRefreshPending(false)
}

// TestBeginBlockerRefreshCadenceFollowsParams proves the drift cadence is the
// parameter and not the calendar: a ten-block period rebuilds at height 9,
// where the launch weekly default would have skipped, and skips at height 4.
func (s *KeeperTestSuite) TestBeginBlockerRefreshCadenceFollowsParams() {
	params := types.DefaultParams()
	params.TaxCapRefreshPeriodBlocks = 10
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// The cap set covers exactly the membership, so its denoms never disagree
	// and the cadence is the only trigger left. Only the stored amount is
	// stale, standing in for the rate drift the cadence exists to true up.
	s.setAssets(chain.SDRBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(7)))

	// Height 4 is inside the shortened period, so the drifted amount survives.
	s.setBlockHeight(4)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.beginBlock())

	staleCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(7), staleCap)

	// Height 9 closes the period, so the same drift is trued up — a block the
	// weekly default would have passed over.
	s.setBlockHeight(9)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.beginBlock())

	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().True(sdrCap.IsZero())
	s.requireTaxCapRefreshPending(false)
}

func (s *KeeperTestSuite) TestBeginBlockerRebuildsCapsAtWeeklyBoundaryWithUnchangedMembership() {
	// The default cadence boundary trues up rate drift even while membership
	// sits still. The zero reference cap keeps the rebuild rate-free, so the
	// only observable difference from a skipped block is the replaced amount.
	s.setBlockHeight(int64(chain.BlocksPerWeek) - 1)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())
	s.setAssets(chain.SDRBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(7)))

	s.Require().NoError(s.beginBlock())

	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().True(sdrCap.IsZero())
	s.requireTypedEvent(&types.EventTaxCapsUpdated{TaxCaps: []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.ZeroInt()},
	}})
}

func (s *KeeperTestSuite) TestBeginBlockerBuildsCapsForMembersOnlyWhenReferenceIsNotAMember() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// The reference names a feed, not necessarily a listed asset: membership
	// excludes asdr, yet the rate capture must still include it so the cap
	// can convert out of reference units.
	s.setAssets(chain.KRWBaseDenom, chain.USDBaseDenom)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.KRWBaseDenom,
		chain.USDBaseDenom,
		chain.SDRBaseDenom,
	).Return(oracletypes.RateSet{
		chain.KRWBaseDenom: math.LegacyNewDec(4),
		chain.USDBaseDenom: math.LegacyNewDec(2),
		chain.SDRBaseDenom: math.LegacyOneDec(),
	}, nil)

	s.Require().NoError(s.beginBlock())

	krwCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(4_000_000), krwCap)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)
	// No asset is listed under the reference denomination, so no cap is
	// stored for it: caps exist only for members.
	_, err = s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().Error(err)
	s.requireTaxCapRefreshPending(false)
}

func (s *KeeperTestSuite) TestBeginBlockerSkipsUnavailableTaxCapRates() {
	s.setBlockHeight(1)
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// Only asdr carries a cap, so the member that never got one is observable.
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(1_000_000)))
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)

	s.Require().NoError(s.beginBlock())
	// The skip is silent on the event stream — it is logged, not evented — so
	// the stored caps are its whole observable effect: the member that never
	// got one still has none.
	_, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)
	// No boundary passed, so the cadence flag stays down: this refresh is
	// membership-triggered, and the retry comes from the caps still failing to
	// cover the registry rather than from any recorded state.
	s.requireTaxCapRefreshPending(false)

	// The surviving cap keeps taxing its member; the member that never got a
	// cap is untaxed until one is derived. An oracle outage must not make a
	// healthy denomination untransactable, so the cost of the skip is revenue
	// and never liveness.
	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 10)), tax)

	tax, err = s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
}

// TestBeginBlockerRetriesSkippedRefreshNextBlock proves the skip is a retry
// loop, not a deferral to the next membership move: the caps still disagree
// with the registry, so every subsequent block attempts the rebuild until
// rates return.
func (s *KeeperTestSuite) TestBeginBlockerRetriesSkippedRefreshNextBlock() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	gomock.InOrder(
		s.oracleKeeper.EXPECT().GetRateSet(
			gomock.Any(),
			chain.SDRBaseDenom,
			chain.USDBaseDenom,
		).Return(nil, oracletypes.ErrStaleExchangeRate),
		s.oracleKeeper.EXPECT().GetRateSet(
			gomock.Any(),
			chain.SDRBaseDenom,
			chain.USDBaseDenom,
		).Return(oracletypes.RateSet{
			chain.SDRBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom: math.LegacyNewDec(2),
		}, nil),
	)

	s.Require().NoError(s.beginBlock())
	s.requireTaxCapRefreshPending(false)

	s.Require().NoError(s.beginBlock())
	s.requireTaxCapRefreshPending(false)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)
	s.requireTypedEvent(&types.EventTaxCapsUpdated{TaxCaps: []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.NewInt(1_000_000)},
		{Denom: chain.USDBaseDenom, TaxCap: math.NewInt(2_000_000)},
	}})
}

func (s *KeeperTestSuite) TestBeginBlockerSkipsUnrepresentableTaxCapConversion() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(nil, oracletypes.ErrConversionOutOfRange)

	s.Require().NoError(s.beginBlock())
	_, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)
	s.requireTaxCapRefreshPending(false)
}

// TestBeginBlockerFloorsTruncatedTaxCapAtOneUnit pins the degrade decision: a
// ceiling worth less than one base unit is floored at one rather than stored as
// the zero it truncates to, which would read as uncapped and lift the ceiling a
// small reference cap was asking to tighten. Refusing the conversion instead
// would hold every member's refresh hostage to a rate pair no retry can mend —
// rates this lopsided are a legal steady state, not an outage.
func (s *KeeperTestSuite) TestBeginBlockerFloorsTruncatedTaxCapAtOneUnit() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	// A one-base-unit reference cap converts to half a unit at this rate pair.
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyNewDec(2),
		chain.USDBaseDenom: math.LegacyOneDec(),
	}, nil)

	s.Require().NoError(s.beginBlock())

	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.OneInt(), usdCap)
	s.requireTaxCapRefreshPending(false)
	s.requireTypedEvent(&types.EventTaxCapsUpdated{TaxCaps: []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.OneInt()},
		{Denom: chain.USDBaseDenom, TaxCap: math.OneInt()},
	}})
}

// TestBeginBlockerRetriesSkippedCadenceRefreshAfterBoundary covers the case
// the epoch comparison could not: a cadence-triggered refresh that skips on
// stale rates. Membership never moves and the boundary block passes, so a
// trigger built on those two alone would go quiet for a whole period. Keying
// the cadence to the last successful rebuild instead leaves the work due until
// it actually succeeds.
func (s *KeeperTestSuite) TestBeginBlockerRetriesSkippedCadenceRefreshAfterBoundary() {
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	params.TaxCapRefreshPeriodBlocks = 10
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// The caps already cover membership exactly, so the cadence is the only
	// trigger and only the stale amount is wrong.
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(7)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(7)))
	gomock.InOrder(
		s.oracleKeeper.EXPECT().GetRateSet(
			gomock.Any(),
			chain.SDRBaseDenom,
			chain.USDBaseDenom,
		).Return(nil, oracletypes.ErrStaleExchangeRate),
		s.oracleKeeper.EXPECT().GetRateSet(
			gomock.Any(),
			chain.SDRBaseDenom,
			chain.USDBaseDenom,
		).Return(oracletypes.RateSet{
			chain.SDRBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom: math.LegacyNewDec(2),
		}, nil),
	)

	// Height 9 closes the period and raises the flag, but rates are stale, so
	// the rebuild skips and the flag stays raised.
	s.setBlockHeight(9)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.beginBlock())
	s.requireTaxCapRefreshPending(true)
	staleCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(7), staleCap)

	// Height 10 is mid-period and membership still matches, yet the flag raised
	// at 9 is still up, so the rebuild runs the moment rates return and lowers
	// it.
	s.setBlockHeight(10)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.beginBlock())
	s.requireTaxCapRefreshPending(false)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)
}

func (s *KeeperTestSuite) requireTaxCapRefreshPending(expected bool) {
	pending, err := s.keeper.TaxCapRefreshPending.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected, pending)
}
