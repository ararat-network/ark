package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestCapitalReadsReportTargetGaps() {
	policy := types.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.2")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 20)).AnyTimes()
	s.insuranceRecognised = math.NewInt(5)

	// Liability is 100, so the targets are 50, 25 and 20.
	required, err := s.keeper.RequiredReserveCapital(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(25), required)

	bufferGap, err := s.keeper.RedemptionBufferShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(30), bufferGap)

	// Insurance is measured against its recognised capital, which the mock
	// answers, and never against its module balance — no balance expectation is
	// stubbed for it, so a handler reaching for one fails here.
	insuranceGap, err := s.keeper.InsuranceShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(15), insuranceGap)

	// The gap is one-sided: a fund at its target is short nothing, and one above
	// it does not report a negative requirement to be netted off elsewhere.
	for _, recognised := range []int64{20, 90} {
		s.insuranceRecognised = math.NewInt(recognised)
		insuranceGap, err = s.keeper.InsuranceShortfall(s.ctx)
		s.Require().NoError(err)
		s.Require().True(insuranceGap.IsZero(), "recognised %d", recognised)
	}
}

// TestBurnBoundRefusesIncompleteValuation pins the one figure that still
// becomes unavailable. Every way an incomplete valuation still returns a number
// can understate the aggregate, which understates the requirement and overstates
// the surplus a committee may destroy — so this bound withholds rather than
// letting a burn size itself on a figure that can only err loose.
func (s *KeeperTestSuite) TestBurnBoundRefusesIncompleteValuation() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	// A live member with no rate: the aggregate cannot be valued.
	s.setRates(oracletypes.RateSet{})

	_, err := s.keeper.RequiredReserveCapital(s.ctx)
	s.Require().ErrorContains(err, "incomplete")
	// Withholding is silent: the disclosure belongs to a read that acted on the
	// degraded figure, and this one refused to.
	s.requireNoTypedEvent(&types.EventLiabilityIncomplete{})
}

// TestFundTransferBoundsSizeOnIncompleteValuation pins the other half of the
// split. Both transfer bounds keep answering while the valuation is degraded,
// because the act they bound only moves capital between protocol funds — and
// because the coverage draw is concurrently spending the Buffer against this
// very aggregate, so gating the refill on completeness would leave the drain
// running while the fast response went dark.
func (s *KeeperTestSuite) TestFundTransferBoundsSizeOnIncompleteValuation() {
	policy := types.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.2")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 60)).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 40)).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 20)).AnyTimes()
	s.insuranceRecognised = math.NewInt(5)

	// akrw's feed is gone but its last known rate survives, so the aggregate is
	// the priced 60 plus the 40 it still counts on that rate: the same 100 the
	// complete case sized on, now qualified.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.setLastKnownRates(oracletypes.RateSet{chain.KRWBaseDenom: math.LegacyOneDec()})

	bufferGap, err := s.keeper.RedemptionBufferShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(30), bufferGap)

	insuranceGap, err := s.keeper.InsuranceShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(15), insuranceGap)

	// Every sizing discloses what it was sized on. The event lands in the
	// transaction that asked, so a committee transfer carries the disclosure of
	// the degraded basis it moved against.
	s.requireTypedEvent(&types.EventLiabilityIncomplete{
		ClaimableLiability: chain.NoahDecCoin(math.LegacyNewDec(100)),
		StaleMemberSupply:  []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 40)},
	})
}

// TestFundTransferBoundsDropNeverPricedSupply pins the direction the tolerant
// path errs in when there is no rate at all to count by. The supply leaves the
// aggregate outright, which shrinks the target and the gap with it: the bound
// under-fills rather than over-fills, which is the safe way for a transfer
// ceiling to be wrong.
func (s *KeeperTestSuite) TestFundTransferBoundsDropNeverPricedSupply() {
	policy := types.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 60)).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 40)).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()

	// akrw was never priced, so nothing counts it: the aggregate is 60, not 100.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.setLastKnownRates(oracletypes.RateSet{})

	bufferGap, err := s.keeper.RedemptionBufferShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(30), bufferGap)
}

// usdSettlementPlan is a valid open plan whose activation height is still in
// the future relative to the suite's block height, so tests exercising it are
// also exercising the ungated plan read.
func usdSettlementPlan() assettypes.SettlementPlan {
	return assettypes.SettlementPlan{
		Denom: chain.USDBaseDenom,
		// Two NOAH per unit of settled ausd.
		RedemptionRate:        math.LegacyNewDec(2),
		OpenedHeight:          1,
		ActivationHeight:      1_000,
		EarliestClosingHeight: 1100,
	}
}

// TestSelfHeldPaperMovesFlowsButNotCommitteeBounds pins the basis split that
// makes netting safe. Ark-issued paper parked in the strategic Reserve nets out
// of the flows — redemption coverage and the waterfall's fund gaps — because no
// claim can arrive from it. It stays inside every bound on a committee act,
// because the committee those bounds constrain can deploy that paper straight
// back into circulation: a bound that loosened when its own subject parked
// paper would be no bound at all.
func (s *KeeperTestSuite) TestSelfHeldPaperMovesFlowsButNotCommitteeBounds() {
	policy := types.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.2")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 20)).AnyTimes()
	s.insuranceRecognised = math.NewInt(5)

	// A fifth of the float is bought back and parked: gross stays 100, net 80.
	s.reserveHoldings = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20))

	// Bounds read gross. The requirement is a quarter of 100, not of 80 — the
	// committee cannot loosen its own burn bound by 5 by parking paper.
	required, err := s.keeper.RequiredReserveCapital(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(25), required)

	// Both committee-transfer bounds hold on the same basis: 50 − 20 and
	// 20 − 5 against gross, unmoved by the parked paper.
	bufferGap, err := s.keeper.RedemptionBufferShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(30), bufferGap)

	insuranceGap, err := s.keeper.InsuranceShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(15), insuranceGap)

	// The flow reads net. Coverage is the Buffer over the claims that can
	// actually arrive: the basis is the net 80 plus the 40 the block retired,
	// so 20/120 of a 40-NOAH output draws 6. On the gross basis of 100 the
	// denominator would have been 140 and the draw 5 — the difference is the
	// parked paper's coverage share returning to real holders.
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 6)),
	).Return(nil)
	burn, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		GrossOffer:        math.ZeroInt(),
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.NewInt(40),
		RedeemedValue:     math.LegacyNewDec(40),
	})
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(6), burn)
}

// TestCommitteeBoundsScaleWithExposure pins the multiplier's reach into
// x/reserve. Every direction it moves a bound is the conservative one: the
// Reserve requirement rises, which shrinks the surplus a committee may burn,
// and the two fund shortfalls rise, which widens the transfers a committee may
// make into the funds that absorb a run.
//
// The bound stays legitimate because the multiplier is protocol-computed from
// supply, oracle rates, and settled flow — none of which the bounded committee
// can set — so this does not loosen a bound on state its actor can reverse.
func (s *KeeperTestSuite) TestCommitteeBoundsScaleWithExposure() {
	policy := types.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.2")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
	s.setMultiplier("2")
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 20)).AnyTimes()
	s.insuranceRecognised = math.NewInt(5)

	// Liability 100 doubled is 200, so the targets are 100, 50 and 40 — each
	// twice what TestCapitalReadsReportTargetGaps sees unscaled.
	required, err := s.keeper.RequiredReserveCapital(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(50), required)

	bufferGap, err := s.keeper.RedemptionBufferShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(80), bufferGap)

	insuranceGap, err := s.keeper.InsuranceShortfall(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(35), insuranceGap)
}
