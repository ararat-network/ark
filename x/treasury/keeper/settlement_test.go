package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	claimstypes "ark/x/claims/types"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	reservetypes "ark/x/reserve/types"
	"ark/x/treasury/types"
)

// expansionTotals is a block that only expanded: principal already net of the
// spread its conversions burned.
func expansionTotals(eligible int64) markettypes.ConversionTotals {
	return markettypes.ConversionTotals{
		EligiblePrincipal: math.NewInt(eligible),
		RedemptionOutput:  math.ZeroInt(),
		RedeemedValue:     math.LegacyZeroDec(),
	}
}

// redemptionTotals is a block that only redeemed.
func redemptionTotals(output int64, redeemedValue string) markettypes.ConversionTotals {
	return markettypes.ConversionTotals{
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.NewInt(output),
		RedeemedValue:     math.LegacyMustNewDecFromStr(redeemedValue),
	}
}

func (s *KeeperTestSuite) bufferAddress() sdk.AccAddress {
	return authtypes.NewModuleAddress(types.RedemptionBufferName)
}

// expectBufferBalances queues the Buffer balance reads in order. Settlement
// reads it once to size the waterfall and again after crediting, so a mixed
// block must see the credit it just made.
func (s *KeeperTestSuite) expectBufferBalances(balances ...int64) {
	calls := make([]any, 0, len(balances))
	for _, balance := range balances {
		calls = append(calls, s.bankKeeper.EXPECT().
			GetBalance(gomock.Any(), s.bufferAddress(), chain.NoahBaseDenom).
			Return(sdk.NewInt64Coin(chain.NoahBaseDenom, balance)))
	}
	gomock.InOrder(calls...)
}

// TestSettleConversionsIdleBlockValuesNothing pins the licence to skip: a block
// with no conversion must not scan the registry at all, which the mocks enforce
// by expecting no supply read.
func (s *KeeperTestSuite) TestSettleConversionsIdleBlockValuesNothing() {
	s.setAssets(chain.USDBaseDenom)

	burn, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		EligiblePrincipal: math.ZeroInt(),
		RedemptionOutput:  math.ZeroInt(),
		RedeemedValue:     math.LegacyZeroDec(),
	})
	s.Require().NoError(err)
	s.Require().True(burn.IsZero())
	s.requireNoTypedEvent(&types.EventExpansionAllocated{})
}

func (s *KeeperTestSuite) TestSettleConversionsRejectsIncoherentTotals() {
	tests := []struct {
		name    string
		totals  markettypes.ConversionTotals
		wantErr string
	}{
		{
			name: "output exceeds the liability it retired",
			totals: markettypes.ConversionTotals{
				EligiblePrincipal: math.ZeroInt(),
				RedemptionOutput:  math.NewInt(11),
				RedeemedValue:     math.LegacyNewDec(10),
			},
			wantErr: "exceeds the redeemed liability",
		},
		{
			name: "unset principal",
			totals: markettypes.ConversionTotals{
				RedemptionOutput: math.ZeroInt(),
				RedeemedValue:    math.LegacyZeroDec(),
			},
			wantErr: "incomplete",
		},
		{
			name: "negative principal",
			totals: markettypes.ConversionTotals{
				EligiblePrincipal: math.NewInt(-1),
				RedemptionOutput:  math.ZeroInt(),
				RedeemedValue:     math.LegacyZeroDec(),
			},
			wantErr: "cannot be negative",
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			_, err := s.keeper.SettleConversions(s.ctx, test.totals)
			s.Require().ErrorContains(err, test.wantErr)
		})
	}
}

// TestSettleConversionsWaterfallsPrincipalOnce covers the whole block's
// principal placed against one valuation: gaps bind in order and the remainder
// burns.
func (s *KeeperTestSuite) TestSettleConversionsWaterfallsPrincipalOnce() {
	policy := types.DefaultMonetaryPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	// Liability 100 implies targets 50 / 25 / 10. The Buffer holds 30 and the
	// Reserve 25, so only the Buffer and Insurance are owed anything.
	s.expectBufferBalances(30)
	s.setReserveRecognised(25)
	s.setInsuranceRecognised(0)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 20)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, claimstypes.InsuranceName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 10)),
	).Return(nil)

	burn, err := s.keeper.SettleConversions(s.ctx, expansionTotals(45))
	s.Require().NoError(err)
	// 20 fills the Buffer gap, the Reserve is already at target, 10 fills
	// Insurance, and the remaining 15 overflows into the burn.
	s.Require().Equal(math.NewInt(15), burn)
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                  chain.NoahBaseDenom,
		RedemptionBufferCredit: math.NewInt(20),
		StrategicReserveCredit: math.ZeroInt(),
		InsuranceCredit:        math.NewInt(10),
		OverflowBurn:           math.NewInt(15),
	})
}

// TestSettleConversionsParksWholeBlockOnIncompleteValuation covers the degraded
// mode at its new scope: one unpriced member parks every conversion's
// principal, where per-swap settlement would have waterfalled the ones that ran
// before the failure.
func (s *KeeperTestSuite) TestSettleConversionsParksWholeBlockOnIncompleteValuation() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 0))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 10))
	s.setRates(oracletypes.RateSet{})
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, reservetypes.StrategicReserveName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 60)),
	).Return(nil)

	burn, err := s.keeper.SettleConversions(s.ctx, expansionTotals(60))
	s.Require().NoError(err)
	// Nothing overflows: the whole principal is parked, so settlement burns
	// nothing at all.
	s.Require().True(burn.IsZero())
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                  chain.NoahBaseDenom,
		RedemptionBufferCredit: math.ZeroInt(),
		StrategicReserveCredit: math.NewInt(60),
		InsuranceCredit:        math.ZeroInt(),
		OverflowBurn:           math.ZeroInt(),
	})
	s.requireTypedEvent(&types.EventLiabilityIncomplete{
		ClaimableLiability: chain.NoahDecCoin(math.LegacyZeroDec()),
		StaleMemberSupply:  []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 10)},
	})
}

// TestSettleConversionsDrawsCoverageAgainstPreBurnBasis pins the basis
// reconstruction: the block's scan is post-burn, so the redeemed liability is
// added back to price coverage on what claims existed when the redemptions
// quoted.
func (s *KeeperTestSuite) TestSettleConversionsDrawsCoverageAgainstPreBurnBasis() {
	s.setAssets(chain.USDBaseDenom)
	// 75 ausd at two per NOAH is 37.5 of post-burn liability; the block retired
	// 12.5, so the pre-burn basis is 50.
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 75))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyNewDec(2)})
	s.expectBufferBalances(40)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 8)),
	).Return(nil)

	burn, err := s.keeper.SettleConversions(s.ctx, redemptionTotals(10, "12.5"))
	s.Require().NoError(err)
	// Coverage 40/50 of a 10 output pays 8, which Market burns against the
	// output it already minted.
	s.Require().Equal(math.NewInt(8), burn)
	s.requireTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:   chain.NoahBaseDenom,
		Payment: math.NewInt(8),
	})
}

// TestSettleConversionsCapsCoverageAtOne covers a Buffer larger than the basis:
// the draw is the whole output and never more, so no redemption can pull more
// NOAH out of the Buffer than it minted.
func (s *KeeperTestSuite) TestSettleConversionsCapsCoverageAtOne() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 10))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.expectBufferBalances(1_000)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 20)),
	).Return(nil)

	burn, err := s.keeper.SettleConversions(s.ctx, redemptionTotals(20, "20"))
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(20), burn)
}

// TestSettleConversionsCreditsBufferBeforeDrawingIt covers the ordering
// decision: a block's expansions replenish the Buffer its redemptions then draw
// against, which is what per-swap settlement did whenever an expansion happened
// to precede a redemption.
func (s *KeeperTestSuite) TestSettleConversionsCreditsBufferBeforeDrawingIt() {
	policy := types.DefaultMonetaryPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyZeroDec()
	policy.InsuranceTargetRatio = math.LegacyZeroDec()
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	// Liability 100 gives a Buffer target of 50 against a balance of 10, so the
	// 40 of principal exactly fills the gap — and the draw then reads 50.
	s.expectBufferBalances(10, 50)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 40)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 8)),
	).Return(nil)

	burn, err := s.keeper.SettleConversions(s.ctx, markettypes.ConversionTotals{
		EligiblePrincipal: math.NewInt(40),
		RedemptionOutput:  math.NewInt(20),
		RedeemedValue:     math.LegacyNewDec(20),
	})
	s.Require().NoError(err)
	// Coverage is 50/120 of a 20 output — the replenished Buffer over a basis of
	// 100 post-burn liability plus the 20 retired — paying 8. Nothing overflows,
	// so the whole burn is the draw.
	s.Require().Equal(math.NewInt(8), burn)
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                  chain.NoahBaseDenom,
		RedemptionBufferCredit: math.NewInt(40),
		StrategicReserveCredit: math.ZeroInt(),
		InsuranceCredit:        math.ZeroInt(),
		OverflowBurn:           math.ZeroInt(),
	})
	s.requireTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:   chain.NoahBaseDenom,
		Payment: math.NewInt(8),
	})
}

// TestSettleConversionsDrawsUnderIncompleteValuation pins that coverage is not
// switched off by a suspension elsewhere: the aggregate already excludes supply
// that cannot redeem, so healthy exits keep their share during the contagion
// the Buffer exists for.
func (s *KeeperTestSuite) TestSettleConversionsDrawsUnderIncompleteValuation() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 30))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 10))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.expectBufferBalances(20)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)),
	).Return(nil)

	burn, err := s.keeper.SettleConversions(s.ctx, redemptionTotals(10, "10"))
	s.Require().NoError(err)
	// akrw is unpriced, so the aggregate is the 30 of ausd; the basis is 40 and
	// coverage 20/40 pays 5.
	s.Require().Equal(math.NewInt(5), burn)
	s.requireTypedEvent(&types.EventLiabilityIncomplete{
		ClaimableLiability: chain.NoahDecCoin(math.LegacyNewDec(30)),
		StaleMemberSupply:  []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 10)},
	})
}

// TestSettleConversionsNetsReserveHeldPaperFromCoverage covers the flow basis:
// paper the Reserve holds has no claimant, so it leaves the denominator the
// draw divides by, exactly as the per-swap draw netted it.
func (s *KeeperTestSuite) TestSettleConversionsNetsReserveHeldPaperFromCoverage() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.reserveHoldings = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 60))
	s.expectBufferBalances(30)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 6)),
	).Return(nil)

	burn, err := s.keeper.SettleConversions(s.ctx, redemptionTotals(10, "10"))
	s.Require().NoError(err)
	// Net liability is 100 less the 60 the Reserve holds, so the basis is 50 and
	// coverage 30/50 pays 6.
	s.Require().Equal(math.NewInt(6), burn)
}
