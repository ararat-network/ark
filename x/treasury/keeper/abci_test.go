package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestBeginBlockerSkipsRewardFundingAtGenesisHeight() {
	s.setBlockHeight(1)

	s.Require().NoError(s.beginBlock())
	s.requireDefaultRewardFunding()
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
	s.expectValidatorFees(sdk.NewCoins())

	s.Require().NoError(s.beginBlock())
	s.requireNoTypedEvent(&types.EventLiabilityIncomplete{})
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
	s.oracleKeeper.EXPECT().GetAvailableRateSet(
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
	s.requireTypedEvent(&types.EventTaxCapsRefreshed{TaxCaps: []types.TaxCap{
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
	// Uncapped keeps the boundary rebuild rate-free, so the cadence is the
	// only thing under test.
	params.ReferenceTaxCap.Amount = math.ZeroInt()
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
	// sits still. An explicitly zero reference cap keeps the rebuild
	// rate-free, so the only observable difference from a skipped block is
	// the replaced amount.
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.ZeroInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setBlockHeight(int64(chain.BlocksPerWeek) - 1)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())
	s.setAssets(chain.SDRBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(7)))
	// A weekly boundary is also an hourly one, so this block recomputes the
	// exposure multiplier alongside the cap rebuild. The recomputation is not
	// what this test is about, but its reads have to be answered.
	s.expectExposureUpdateReads()

	s.Require().NoError(s.beginBlock())

	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().True(sdrCap.IsZero())
	s.requireTypedEvent(&types.EventTaxCapsRefreshed{TaxCaps: []types.TaxCap{
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
	s.oracleKeeper.EXPECT().GetAvailableRateSet(
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

// TestBeginBlockerCoversMembersLackingUsableRates pins all three verdicts of
// one partial pass: a member whose rate serves is derived, a covered member
// whose rate does not keeps its cap, and an uncovered member whose rate does
// not is seeded at the unconverted reference amount rather than left exempt.
// The pass is incomplete, so the refresh stays owed.
func (s *KeeperTestSuite) TestBeginBlockerCoversMembersLackingUsableRates() {
	s.setBlockHeight(1)
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// akrw holds a kept cap and its rate is unavailable; ausd awaits its
	// first cap; asdr is the reference member, derivable without any rate.
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.KRWBaseDenom, math.NewInt(7)))
	s.setAssets(chain.KRWBaseDenom, chain.SDRBaseDenom, chain.USDBaseDenom)
	s.oracleKeeper.EXPECT().GetAvailableRateSet(
		gomock.Any(),
		chain.KRWBaseDenom,
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
	}, nil)

	s.Require().NoError(s.beginBlock())
	krwCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(7), krwCap)
	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), sdrCap)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), usdCap)
	s.requireTaxCapRefreshPending(true)
	// The event carries what the pass wrote — the derived reference member
	// and the seed — never the kept cap it left alone.
	s.requireTypedEvent(&types.EventTaxCapsRefreshed{TaxCaps: []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.NewInt(1_000_000)},
		{Denom: chain.USDBaseDenom, TaxCap: math.NewInt(1_000_000)},
	}})

	// Every member taxes through the outage: the kept cap keeps clamping —
	// 10 would be owed at the rate, the kept cap of 7 binds — and the seeded
	// arrival pays under its seed. An oracle outage must not make a healthy
	// denomination untransactable — nor tax-exempt.
	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 7)), tax)

	tax, err = s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10)), tax)
}

// TestBeginBlockerRetriesIncompleteRefreshNextBlock proves a seed is a
// placeholder, not a resting state: the member whose rate is unavailable is
// seeded, the flag stays raised, and the next block's pass re-expresses the
// whole set once rates return.
func (s *KeeperTestSuite) TestBeginBlockerRetriesIncompleteRefreshNextBlock() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	gomock.InOrder(
		s.oracleKeeper.EXPECT().GetAvailableRateSet(
			gomock.Any(),
			chain.SDRBaseDenom,
			chain.USDBaseDenom,
		).Return(oracletypes.RateSet{
			chain.SDRBaseDenom: math.LegacyOneDec(),
		}, nil),
		s.oracleKeeper.EXPECT().GetAvailableRateSet(
			gomock.Any(),
			chain.SDRBaseDenom,
			chain.USDBaseDenom,
		).Return(oracletypes.RateSet{
			chain.SDRBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom: math.LegacyNewDec(2),
		}, nil),
	)

	s.Require().NoError(s.beginBlock())
	// The reference member derives without a rate; the member whose rate is
	// unavailable is seeded at the reference amount, so the refresh stays
	// owed.
	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), sdrCap)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), usdCap)
	s.requireTaxCapRefreshPending(true)

	s.Require().NoError(s.beginBlock())
	s.requireTaxCapRefreshPending(false)
	usdCap, err = s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), usdCap)
	s.requireTypedEvent(&types.EventTaxCapsRefreshed{TaxCaps: []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.NewInt(1_000_000)},
		{Denom: chain.USDBaseDenom, TaxCap: math.NewInt(2_000_000)},
	}})
}

func (s *KeeperTestSuite) TestBeginBlockerCoversUnrepresentableTaxCapConversion() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.setAssets(chain.SDRBaseDenom, chain.USDBaseDenom)
	// The rate is present but the cross is unrepresentable: 10^6 reference
	// units times a 10^72 rate leaves the decimal domain mid-conversion.
	s.oracleKeeper.EXPECT().GetAvailableRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDec(10).Power(72),
	}, nil)

	s.Require().NoError(s.beginBlock())
	// An unrepresentable cross is covered exactly like a missing rate: the
	// uncovered member is seeded rather than left exempt, and the seed keeps
	// the refresh owed.
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), usdCap)
	s.requireTaxCapRefreshPending(true)
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
	s.oracleKeeper.EXPECT().GetAvailableRateSet(
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
	s.requireTypedEvent(&types.EventTaxCapsRefreshed{TaxCaps: []types.TaxCap{
		{Denom: chain.SDRBaseDenom, TaxCap: math.OneInt()},
		{Denom: chain.USDBaseDenom, TaxCap: math.OneInt()},
	}})
}

// TestBeginBlockerRetriesIncompleteCadenceRefreshAfterBoundary covers the
// case the epoch comparison could not: a cadence-triggered refresh whose
// rates cannot serve it. Membership never moves and the boundary block
// passes, so a trigger built on those two alone would go quiet for a whole
// period. Keying the cadence to the last complete pass instead leaves the
// work due until it actually completes.
func (s *KeeperTestSuite) TestBeginBlockerRetriesIncompleteCadenceRefreshAfterBoundary() {
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	params.TaxCapRefreshPeriodBlocks = 10
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	// The caps already cover membership exactly, so the cadence is the only
	// trigger and only the stale amounts are wrong. The non-reference member
	// is what keeps the boundary pass incomplete: the reference member
	// derives without a rate, so covered akrw is what the empty rate set
	// leaves kept.
	s.setAssets(chain.KRWBaseDenom, chain.SDRBaseDenom)
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.KRWBaseDenom, math.NewInt(7)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(7)))
	gomock.InOrder(
		s.oracleKeeper.EXPECT().GetAvailableRateSet(
			gomock.Any(),
			chain.KRWBaseDenom,
			chain.SDRBaseDenom,
		).Return(oracletypes.RateSet{}, nil),
		s.oracleKeeper.EXPECT().GetAvailableRateSet(
			gomock.Any(),
			chain.KRWBaseDenom,
			chain.SDRBaseDenom,
		).Return(oracletypes.RateSet{
			chain.KRWBaseDenom: math.LegacyNewDec(2),
			chain.SDRBaseDenom: math.LegacyOneDec(),
		}, nil),
	)

	// Height 9 closes the period and raises the flag, but no rate serves the
	// non-reference member, so its kept cap stands and the flag stays raised.
	s.setBlockHeight(9)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.beginBlock())
	s.requireTaxCapRefreshPending(true)
	staleCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(7), staleCap)

	// Height 10 is mid-period and membership still matches, yet the flag
	// raised at 9 is still up, so the pass completes the moment rates return
	// and lowers it.
	s.setBlockHeight(10)
	s.setRewardFunding(rewardFunding(5, 0, 0, 0))
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.beginBlock())
	s.requireTaxCapRefreshPending(false)
	krwCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.KRWBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(2_000_000), krwCap)
}

func (s *KeeperTestSuite) requireTaxCapRefreshPending(expected bool) {
	pending, err := s.keeper.TaxCapRefreshPending.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(expected, pending)
}
