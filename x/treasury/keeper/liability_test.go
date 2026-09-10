package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/keeper"
	"github.com/ararat-network/ark/x/treasury/types"
)

// requireLiabilityValuation asserts the aggregate the block would settle
// against. Treasury keeps that figure execution-local — settlement hands Market
// only a burn — so tests read it through the status query, which folds the
// registry the same way settlement does.
func (s *KeeperTestSuite) requireLiabilityValuation(expected math.LegacyDec, complete bool) {
	s.T().Helper()
	gross, selfHeld := s.liabilityValuation(complete)
	s.Require().Equal(expected, gross)
	// Tests that park protocol paper assert the netting through
	// requireNetLiabilityValuation; every other test expects nothing self-held.
	s.Require().True(selfHeld.IsZero(), "unexpected self-held liability %s", selfHeld)
}

// requireNetLiabilityValuation asserts both bases: what the fold counted, and
// the part of it the Reserve holds and no claim can arrive from.
func (s *KeeperTestSuite) requireNetLiabilityValuation(
	expectedGross math.LegacyDec,
	expectedSelfHeld math.LegacyDec,
	complete bool,
) {
	s.T().Helper()
	gross, selfHeld := s.liabilityValuation(complete)
	s.Require().Equal(expectedGross, gross)
	s.Require().Equal(expectedSelfHeld, selfHeld)
}

func (s *KeeperTestSuite) liabilityValuation(complete bool) (math.LegacyDec, math.LegacyDec) {
	s.T().Helper()
	// The query reports fund balances beside the aggregate. They are not what
	// these tests assert, and the Reserve's own holdings keep the
	// address-specific expectation the fixture registers first.
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
	status, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
		s.ctx,
		&types.QueryFundStatusRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(
		complete,
		len(status.StaleMemberSupply) == 0 && len(status.UntrustedSuspendedSupply) == 0,
	)
	gross := status.PricedLiability.Amount.
		Add(status.SettlementLiability.Amount).
		Add(status.StalePricedLiability.Amount)

	return gross, status.SelfHeldLiability.Amount
}

// TestLiabilityFeedOutageLeavesCoverageUnchanged pins that an outage elsewhere
// costs a healthy redeemer nothing. The stale member is still counted at its
// last known rate, so the denominator coverage divides by is unchanged — only
// the completeness flag moves, and that governs fund targets, not draws.
func (s *KeeperTestSuite) TestLiabilityFeedOutageLeavesCoverageUnchanged() {
	settleOnce := func() math.Int {
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
			Return(sdk.NewInt64Coin(chain.USDBaseDenom, 80)).Times(1)
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
			Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
		s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
			Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)).Times(1)
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
			gomock.Any(),
		).Return(nil).AnyTimes()

		// The block retired 20 of the ausd float, so the post-burn scan sees 80
		// and the pre-burn basis it reconstructs is 200.
		drawn, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
			GrossOffer:        math.ZeroInt(),
			EligiblePrincipal: math.ZeroInt(),
			RedemptionOutput:  math.NewInt(20),
			RedeemedValue:     math.LegacyNewDec(20),
		})
		s.Require().NoError(err)
		return drawn
	}

	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	})
	// Coverage 50/200 of the 20-NOAH output.
	healthy := settleOnce()
	s.Require().Equal(math.NewInt(5), healthy)

	// The feed drops. Nothing about akrw's obligation changed, and the rate
	// that priced it a block ago is still on record.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.setLastKnownRates(oracletypes.RateSet{chain.KRWBaseDenom: math.LegacyOneDec()})

	during := settleOnce()
	s.Require().Equal(healthy, during, "a feed outage elsewhere must not change this redemption")
	// The aggregate is whole, but no fresh rate stood behind part of it, so the
	// flag is false and fund targets stay unsized.
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.requireLiabilityValuation(math.LegacyNewDec(200), false)
}

// TestLiabilityRecognisesSettlementPricedSupply proves the fold carries
// suspended supply at its settlement plan's committed rate — including a plan
// that has not reached its activation height, because the plan read is
// deliberately ungated — so a suspended denomination leaves the valuation
// complete without any oracle rate behind it.
func (s *KeeperTestSuite) TestLiabilityRecognisesSettlementPricedSupply() {
	s.setAssets()
	s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.plans[chain.USDBaseDenom] = usdSettlementPlan()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)

	// 100 units at the committed 2-NOAH rate.
	s.requireLiabilityValuation(math.LegacyNewDec(200), true)
}

// TestLiabilityCountsDustSupplyAtItsWorth pins measurement semantics for the
// priced partition: a hyperinflated member's supply is counted at exactly what
// it is worth, however little. Valuing in NOAH multiplies, so a whole-unit
// supply at the smallest representable rate cannot underflow — the aggregate
// carries the dust rather than dropping it or marking itself unavailable, and
// one hyperinflated denomination cannot disable settlement chain-wide.
func (s *KeeperTestSuite) TestLiabilityCountsDustSupplyAtItsWorth() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 1)).Times(1)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacySmallestDec(),
	})

	s.requireLiabilityValuation(math.LegacyNewDec(100).Add(math.LegacySmallestDec()), true)
}

// TestLiabilityFailsOnUnrepresentableConversion pins arithmetic out of range as
// a hard failure rather than an incomplete valuation. A member the partition
// prices, whose supply at its rate leaves Dec range, is state past the
// supported domain: unlike a missing rate it does not return on the next block,
// so reporting it as unavailable would retire the Buffer permanently behind a
// flag that fires for benign reasons. Incompleteness is reserved for exposure
// no honest rate could value.
//
// Reaching settlement, this fails the block. That is the halt class the
// deferred design accepts: the arithmetic is unreachable through any conversion
// a trader can make, and settling on a figure known to be wrong is worse.
func (s *KeeperTestSuite) TestLiabilityFailsOnUnrepresentableConversion() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewCoin(chain.USDBaseDenom, math.NewIntWithDecimal(1, 60))).Times(1)
	// At 1e18 NOAH per ausd, 1e60 base units value to 1e78 NOAH — beyond
	// LegacyDec range.
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyNewDec(10).Power(18),
	})

	_, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		GrossOffer:        math.NewInt(1),
		EligiblePrincipal: math.NewInt(1),
		RedemptionOutput:  math.ZeroInt(),
		RedeemedValue:     math.LegacyZeroDec(),
	})
	s.Require().ErrorContains(err, "valuing liability ausd")
	s.Require().ErrorIs(err, oracletypes.ErrConversionOutOfRange)
}

// TestLiabilityExcludesUnpricedMemberFromClaimable pins settlement under
// partial information: the stale member is excluded from the claimable
// aggregate, and the block's draw funds healthy exits at coverage of that
// aggregate — a smaller denominator than the complete one, so a lapse elsewhere
// raises per-exit funding instead of switching the Buffer off.
func (s *KeeperTestSuite) TestLiabilityExcludesUnpricedMemberFromClaimable() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 90)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)).Times(1)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)),
	).Return(nil)

	// The block retired 10 of the ausd float, so the basis is the 90 still
	// outstanding plus that 10. Coverage 50/100 of a 10-NOAH output pays 5;
	// with akrw priced the denominator would have been 200 and the draw 2.
	drawn, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		GrossOffer:        math.ZeroInt(),
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.NewInt(10),
		RedeemedValue:     math.LegacyNewDec(10),
	})
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(5), drawn)
	// Settlement disclosed the degraded valuation once, naming akrw and the 90
	// its own fold counted.
	s.requireTypedEvent(&types.EventLiabilityIncomplete{
		ClaimableLiability: chain.NoahDecCoin(math.LegacyNewDec(90)),
		StaleMemberSupply:  []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 100)},
	})
}

func (s *KeeperTestSuite) TestFundStatusPartitionsLiabilityByLifecycleStatus() {
	writeOff := func(denom string, amount int64) types.WrittenOffExposure {
		return types.WrittenOffExposure{
			OutstandingSupply: sdk.NewInt64Coin(denom, amount),
			WriteOffVersion:   1,
		}
	}
	tests := []struct {
		name            string
		seed            func()
		supplies        map[string]int64
		expectRates     func()
		wantPriced      string
		wantSettlement  string
		wantStale       string
		wantUntrusted   sdk.Coins
		wantWrittenOff  []types.WrittenOffExposure
		wantStaleSupply sdk.Coins
	}{
		{
			name: "all active supply is oracle-priced",
			seed: func() {
				s.setAssets(chain.KRWBaseDenom, chain.USDBaseDenom)
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 100, chain.USDBaseDenom: 100},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{
					chain.KRWBaseDenom: math.LegacyNewDecWithPrec(5, 1),
					chain.USDBaseDenom: math.LegacyOneDec(),
				})
			},
			wantPriced: "150",
		},
		{
			name: "issuance-halted supply stays priced",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED)
			},
			supplies: map[string]int64{chain.USDBaseDenom: 100},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
			},
			wantPriced: "100",
		},
		{
			// The fixture plan's activation height is far in the future: an
			// open-but-unactivated plan already carries recognition, because
			// the plan read is ungated by design.
			name: "suspended supply under an open plan is settlement-priced",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
				s.plans[chain.USDBaseDenom] = usdSettlementPlan()
			},
			supplies:       map[string]int64{chain.USDBaseDenom: 100},
			wantSettlement: "200",
		},
		{
			name: "suspended supply without a plan is untrusted exposure",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
			},
			supplies:      map[string]int64{chain.USDBaseDenom: 100},
			wantUntrusted: []sdk.Coin{sdk.NewInt64Coin(chain.USDBaseDenom, 100)},
		},
		{
			name: "written-off supply is disclosed but stays derecognized",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
			},
			supplies:       map[string]int64{chain.USDBaseDenom: 60},
			wantWrittenOff: []types.WrittenOffExposure{writeOff(chain.USDBaseDenom, 60)},
		},
		{
			// Positive RETIRED supply, or supply under a status this fold does
			// not recognise, cannot exist in practice; if it ever does, it must
			// not leak into any bucket.
			name: "retired and unrecognised supply is invisible",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED)
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_RETIRED)
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 50, chain.USDBaseDenom: 70},
		},
		{
			name: "zero supply is skipped entirely",
			seed: func() {
				s.setAssets(chain.USDBaseDenom)
			},
			supplies: map[string]int64{chain.USDBaseDenom: 0},
		},
		{
			name: "priced and settlement-priced sum into the recognised total",
			seed: func() {
				s.setAssets(chain.USDBaseDenom)
				s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
				s.plans[chain.KRWBaseDenom] = assettypes.SettlementPlan{
					Denom:                 chain.KRWBaseDenom,
					RedemptionRate:        math.LegacyNewDec(2),
					OpenedHeight:          1,
					ActivationHeight:      1_000,
					EarliestClosingHeight: 1100,
				}
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 50, chain.USDBaseDenom: 100},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
			},
			wantPriced:     "100",
			wantSettlement: "100",
		},
		{
			name: "a stale member is disclosed beside the surviving lists",
			seed: func() {
				s.setAssets(chain.KRWBaseDenom)
				s.seedAsset(chain.XDRBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
			},
			supplies: map[string]int64{
				chain.KRWBaseDenom: 100,
				chain.XDRBaseDenom: 40,
				chain.USDBaseDenom: 60,
			},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{})
			},
			wantUntrusted:   []sdk.Coin{sdk.NewInt64Coin(chain.XDRBaseDenom, 40)},
			wantWrittenOff:  []types.WrittenOffExposure{writeOff(chain.USDBaseDenom, 60)},
			wantStaleSupply: []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 100)},
		},
		{
			// The routine degradation: one feed lapses and every other member
			// keeps its measured value. Reporting the healthy 100 as zero would
			// be indistinguishable from every feed being down.
			name: "one stale member leaves the healthy members priced",
			seed: func() {
				s.setAssets(chain.KRWBaseDenom, chain.USDBaseDenom)
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 40, chain.USDBaseDenom: 100},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
			},
			wantPriced:      "100",
			wantStaleSupply: []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 40)},
		},
		{
			// The same lapse, but the Oracle still holds what akrw was worth.
			// The supply stays disclosed and the flag stays false — no fresh
			// rate stood behind it — while the nominal total keeps counting it,
			// so an operator sees the obligation rather than a total that
			// quietly shrank by 80.
			name: "a stale member with a rate on record is valued apart",
			seed: func() {
				s.setAssets(chain.KRWBaseDenom, chain.USDBaseDenom)
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 40, chain.USDBaseDenom: 100},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
				s.setLastKnownRates(oracletypes.RateSet{
					chain.KRWBaseDenom: math.LegacyNewDec(2),
				})
			},
			wantPriced:      "100",
			wantStale:       "80",
			wantStaleSupply: []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 40)},
		},
		{
			// Same shape with the stale member at the other end of the
			// registry's key order. The priced total must not depend on where
			// the lapse falls in the walk — the failure the removed
			// short-circuit would have reintroduced, silently dropping every
			// member enumerated after it.
			name: "the priced total ignores where the stale member sorts",
			seed: func() {
				s.setAssets(chain.KRWBaseDenom, chain.USDBaseDenom)
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 100, chain.USDBaseDenom: 40},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{chain.KRWBaseDenom: math.LegacyOneDec()})
			},
			wantPriced:      "100",
			wantStaleSupply: []sdk.Coin{sdk.NewInt64Coin(chain.USDBaseDenom, 40)},
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.plans = map[string]assettypes.SettlementPlan{}
			test.seed()
			for denom, amount := range test.supplies {
				s.bankKeeper.EXPECT().GetSupply(s.ctx, denom).
					Return(sdk.NewInt64Coin(denom, amount))
			}
			if test.expectRates != nil {
				test.expectRates()
			}
			// The Subsidy Pool and the Redemption Buffer, the only two funds
			// Treasury reads from Bank directly.
			s.bankKeeper.EXPECT().GetBalance(s.ctx, gomock.Any(), chain.NoahBaseDenom).
				Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

			response, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
				s.ctx,
				&types.QueryFundStatusRequest{},
			)
			s.Require().NoError(err)

			wantPriced := math.LegacyZeroDec()
			if test.wantPriced != "" {
				wantPriced = math.LegacyMustNewDecFromStr(test.wantPriced)
			}
			wantSettlement := math.LegacyZeroDec()
			if test.wantSettlement != "" {
				wantSettlement = math.LegacyMustNewDecFromStr(test.wantSettlement)
			}
			wantStale := math.LegacyZeroDec()
			if test.wantStale != "" {
				wantStale = math.LegacyMustNewDecFromStr(test.wantStale)
			}
			s.Require().Equal(
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, wantStale),
				response.StalePricedLiability,
			)
			s.Require().Equal(
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, wantPriced),
				response.PricedLiability,
			)
			s.Require().Equal(
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, wantSettlement),
				response.SettlementLiability,
			)
			// A case that expects no exclusions leaves its want nil, and the
			// response carries nil for an untouched list — nil and empty are
			// the same disclosure everywhere the report travels.
			s.Require().Equal(test.wantUntrusted, response.UntrustedSuspendedSupply)
			s.Require().Equal(test.wantWrittenOff, response.WrittenOffExposure)
			s.Require().Equal(test.wantStaleSupply, response.StaleMemberSupply)

			// The claimable aggregate is priced plus settlement-priced plus
			// stale-priced, and is reported whether or not it covers every
			// recognised liability: it is the denominator redemption coverage
			// divides by, so an incomplete valuation must still disclose it
			// rather than report a zero that no draw uses. The two exclusion
			// lists asserted above are what qualify it — both empty is what
			// says the valuation covered everything recognised.
			s.Require().Equal(
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, wantPriced.Add(wantSettlement).Add(wantStale)),
				response.NominalLiability,
			)
		})
	}
}

// TestLiabilityNetsSelfHeldSupply pins the netting the two bases exist for.
// Ark-issued paper the strategic Reserve holds is inside the claimable
// aggregate and inside no claim anyone can present: the Reserve has no path to
// Market, and only a committee act returns it to circulation. Flows therefore
// divide by the net figure while every bound on a committee act keeps the gross
// one.
func (s *KeeperTestSuite) TestLiabilityNetsSelfHeldSupply() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	})
	// The Reserve bought back a quarter of the ausd float.
	s.reserveHoldings = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 25))

	// Gross counts all 200 of supply; 25 of it sits in the Reserve.
	s.requireNetLiabilityValuation(math.LegacyNewDec(200), math.LegacyNewDec(25), true)
}

// TestLiabilityNetsSelfHeldOnlyForCountedSupply pins that the two sides of the
// subtraction always agree. A member the aggregate does not count cannot be
// netted out of it, or the netting would remove liability that was never there.
func (s *KeeperTestSuite) TestLiabilityNetsSelfHeldOnlyForCountedSupply() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	// akrw has never been priced, so it is outside the aggregate entirely. The
	// Reserve holds some of both.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.setLastKnownRates(oracletypes.RateSet{})
	s.reserveHoldings = sdk.NewCoins(
		sdk.NewInt64Coin(chain.USDBaseDenom, 25),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 40),
	)

	// Only the counted ausd is netted: the uncounted akrw holding contributes
	// to neither side.
	s.requireNetLiabilityValuation(math.LegacyNewDec(100), math.LegacyNewDec(25), false)
}

// TestFundStatusReportsBothTargetBases pins that the status query publishes the
// split rather than picking a side. The nominal-sized targets are what bounds a
// committee burn or transfer; the net-sized ones are where the next expansion
// will route. A reader given only one could recover the other only by knowing
// the policy ratios and the netting rule, so both are reported and each pair
// differs by exactly that fund's ratio times the self-held liability.
func (s *KeeperTestSuite) TestFundStatusReportsBothTargetBases() {
	policy := types.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.2")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
	// A fifth of the float bought back: gross 100, net 80.
	s.reserveHoldings = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20))

	resp, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(s.ctx, &types.QueryFundStatusRequest{})
	s.Require().NoError(err)

	s.Require().Equal("100.000000000000000000", resp.NominalLiability.Amount.String())
	s.Require().Equal("20.000000000000000000", resp.SelfHeldLiability.Amount.String())
	s.Require().Equal("80.000000000000000000", resp.NetLiability.Amount.String())
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)), sdk.Coins(resp.SelfHeldSupply))

	// Bounds: ratios against the gross 100.
	s.Require().Equal(math.NewInt(50), resp.RedemptionBufferTarget.Amount)
	s.Require().Equal(math.NewInt(25), resp.StrategicReserveTarget.Amount)
	s.Require().Equal(math.NewInt(20), resp.InsuranceTarget.Amount)

	// Routing: the same ratios against the net 80, each below its twin by that
	// ratio times the 20 parked.
	s.Require().Equal(math.NewInt(40), resp.RedemptionBufferNetTarget.Amount)
	s.Require().Equal(math.NewInt(20), resp.StrategicReserveNetTarget.Amount)
	s.Require().Equal(math.NewInt(16), resp.InsuranceNetTarget.Amount)

	// The nominal targets are exactly what the committee bounds read, so the
	// query and the keeper cannot drift apart on which basis is which.
	required, err := s.keeper.RequiredReserveCapital(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(resp.StrategicReserveTarget.Amount, required)
}

// TestFundStatusZeroesBothTargetBasesWhenIncomplete pins that neither basis
// survives an incomplete valuation: a target nobody can size is not a target,
// and the netting does not exempt one set from that rule.
func (s *KeeperTestSuite) TestFundStatusZeroesBothTargetBasesWhenIncomplete() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).AnyTimes()
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
	// akrw has never been priced, so the valuation is incomplete.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.setLastKnownRates(oracletypes.RateSet{})
	s.reserveHoldings = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20))

	resp, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(s.ctx, &types.QueryFundStatusRequest{})
	s.Require().NoError(err)

	// The aggregate figures are still reported — withholding them would blind
	// operators during the only stress that makes them interesting.
	s.Require().Equal("100.000000000000000000", resp.NominalLiability.Amount.String())
	s.Require().Equal("80.000000000000000000", resp.NetLiability.Amount.String())

	for _, target := range []math.Int{
		resp.RedemptionBufferTarget.Amount,
		resp.StrategicReserveTarget.Amount,
		resp.InsuranceTarget.Amount,
		resp.RedemptionBufferNetTarget.Amount,
		resp.StrategicReserveNetTarget.Amount,
		resp.InsuranceNetTarget.Amount,
	} {
		s.Require().True(target.IsZero(), "target %s should be zero under an incomplete valuation", target)
	}
}
