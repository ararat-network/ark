package keeper_test

import (
	"errors"
	"math/big"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	assettypes "ark/x/asset/types"
	claimstypes "ark/x/claims/types"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	reservetypes "ark/x/reserve/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestRouteExpansionRejectsInvalidInputs() {
	tests := []struct {
		name         string
		grossOffer   sdk.Coin
		stableOutput sdk.Coin
		wantErr      string
	}{
		{
			name:         "wrong gross denom",
			grossOffer:   sdk.NewInt64Coin(chain.USDBaseDenom, 1),
			stableOutput: sdk.NewInt64Coin(chain.USDBaseDenom, 1),
			wantErr:      "invalid gross offer",
		},
		{
			name:         "zero gross offer",
			grossOffer:   sdk.NewInt64Coin(chain.NoahBaseDenom, 0),
			stableOutput: sdk.NewInt64Coin(chain.USDBaseDenom, 1),
			wantErr:      "invalid gross offer",
		},
		{
			name:         "negative gross offer",
			grossOffer:   sdk.Coin{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)},
			stableOutput: sdk.NewInt64Coin(chain.USDBaseDenom, 1),
			wantErr:      "invalid gross offer",
		},
		{
			name:         "zero stable output",
			grossOffer:   sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
			stableOutput: sdk.NewInt64Coin(chain.USDBaseDenom, 0),
			wantErr:      "stable output must be positive",
		},
		{
			name:         "malformed stable output",
			grossOffer:   sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
			stableOutput: sdk.Coin{Denom: "BAD DENOM", Amount: math.OneInt()},
			wantErr:      "invalid stable output",
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			_, err := s.keeper.RouteExpansion(
				s.ctx,
				test.grossOffer,
				test.stableOutput,
				oracletypes.RateSet{},
			)
			s.Require().ErrorContains(err, test.wantErr)
		})
	}
}

func (s *KeeperTestSuite) TestRouteExpansionFailsBeforeTransferWhenOutputRateUnavailable() {
	s.setAssets(chain.USDBaseDenom)

	_, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		sdk.NewInt64Coin(chain.USDBaseDenom, 60),
		oracletypes.RateSet{chain.NoahBaseDenom: math.LegacyOneDec()},
	)
	s.Require().ErrorIs(err, oracletypes.ErrUnknownDenom)
}

func (s *KeeperTestSuite) TestRouteExpansionRejectsOutputValueAboveGrossOffer() {
	s.setAssets(chain.USDBaseDenom)

	_, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		sdk.NewInt64Coin(chain.USDBaseDenom, 101),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().ErrorContains(err, "stable output value 101anoah exceeds gross offer 100anoah")
}

func (s *KeeperTestSuite) TestRouteExpansionSkipsZeroCredits() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 10))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 0))
	s.setRates(oracletypes.RateSet{chain.KRWBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0))

	quoteRates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	burned, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		sdk.NewInt64Coin(chain.USDBaseDenom, 60),
		quoteRates,
	)
	s.Require().NoError(err)
	// Zero targets fund nothing, so the whole 60 of eligible principal overflows
	// and joins the 40 spread: the gross offer burns entire. No credit transfer
	// is expected, which the mock enforces by failing on any unexpected send.
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), burned)
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                   chain.NoahBaseDenom,
		RedemptionBufferCredit:  math.ZeroInt(),
		StrategicReserveCredit:  math.ZeroInt(),
		InsuranceCredit:         math.ZeroInt(),
		SpreadAndDustBurn:       math.NewInt(40),
		OverflowBurn:            math.NewInt(60),
		TargetValuationComplete: true,
	})
}

func (s *KeeperTestSuite) TestRouteExpansionUsesTargetWaterfall() {
	policy := types.DefaultMonetaryPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 40))
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, reservetypes.StrategicReserveName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 10)),
	).Return(nil)

	burned, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		sdk.NewInt64Coin(chain.USDBaseDenom, 60),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	// The waterfall funds both gaps out of the 60 eligible principal, so only
	// the 40 spread burns.
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 40), burned)
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                   chain.NoahBaseDenom,
		RedemptionBufferCredit:  math.NewInt(50),
		StrategicReserveCredit:  math.NewInt(10),
		InsuranceCredit:         math.ZeroInt(),
		SpreadAndDustBurn:       math.NewInt(40),
		OverflowBurn:            math.ZeroInt(),
		TargetValuationComplete: true,
	})
}

func (s *KeeperTestSuite) TestRouteExpansionRoundsOnlyFinalAmounts() {
	policy := types.DefaultMonetaryPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.1")
	policy.StrategicReserveTargetRatio = math.LegacyZeroDec()
	policy.InsuranceTargetRatio = math.LegacyZeroDec()
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 0))
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 2)),
	).Return(nil)

	burned, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 20),
		sdk.NewInt64Coin(chain.USDBaseDenom, 101),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyNewDec(10),
		},
	)
	s.Require().NoError(err)
	// The 10.1 output truncates to 10 of eligible principal against a 20 offer,
	// and the 0.1 target ratio rounds the Buffer gap up to 2. The 10 spread and
	// the 8 overflow are audited apart but burn as one movement.
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 18), burned)
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                   chain.NoahBaseDenom,
		RedemptionBufferCredit:  math.NewInt(2),
		StrategicReserveCredit:  math.ZeroInt(),
		InsuranceCredit:         math.ZeroInt(),
		SpreadAndDustBurn:       math.NewInt(10),
		OverflowBurn:            math.NewInt(8),
		TargetValuationComplete: true,
	})
}

// TestRouteExpansionParksPrincipalInReserveOnUnrelatedStaleRate pins the
// custody choice an incomplete valuation forces. A member the quote never
// touched has no rate, so no target can be sized, and the whole eligible
// principal goes to the one fund whose allocation an operator can still revise
// once valuation recovers. No fund status is read, which the mock enforces by
// expecting no balance lookup.
func (s *KeeperTestSuite) TestRouteExpansionParksPrincipalInReserveOnUnrelatedStaleRate() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 10))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 0))
	s.setRates(oracletypes.RateSet{})
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, reservetypes.StrategicReserveName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 60)),
	).Return(nil)

	burned, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
		sdk.NewInt64Coin(chain.USDBaseDenom, 60),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	// An incomplete valuation parks the whole 60 of eligible principal in the
	// Reserve, so nothing overflows and only the 40 spread burns.
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 40), burned)
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                   chain.NoahBaseDenom,
		RedemptionBufferCredit:  math.ZeroInt(),
		StrategicReserveCredit:  math.NewInt(60),
		InsuranceCredit:         math.ZeroInt(),
		SpreadAndDustBurn:       math.NewInt(40),
		OverflowBurn:            math.ZeroInt(),
		TargetValuationComplete: false,
	})
}

// TestRouteExpansionFailsOnAggregateOverflow pins an unrepresentable aggregate
// as fatal to the settlement rather than a conservative fallback. Two members
// each value inside Dec range while their sum does not, so nothing here is
// unpriced — the valuation is complete and the arithmetic is out of domain,
// which the conversion declines to route around. The whole swap rolls back
// instead of silently parking every principal in the Reserve on a liability
// figure the chain could not compute.
func (s *KeeperTestSuite) TestRouteExpansionFailsOnAggregateOverflow() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	largeSupply := new(big.Int).Lsh(big.NewInt(1), uint(math.MaxBitLen-1))
	largeSupply.Add(largeSupply, big.NewInt(1))
	for _, denom := range []string{chain.KRWBaseDenom, chain.USDBaseDenom} {
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), denom).
			Return(sdk.NewCoin(denom, math.NewIntFromBigInt(largeSupply)))
	}

	_, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
		sdk.NewInt64Coin(chain.USDBaseDenom, 1),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
			chain.KRWBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().ErrorContains(err, "summing liability")
	s.Require().ErrorIs(err, decimal.ErrOutOfRange)
}

func (s *KeeperTestSuite) TestRouteExpansionPropagatesEachFixedTransferFailure() {
	tests := []struct {
		name       string
		failModule string
	}{
		{name: "redemption buffer", failModule: types.RedemptionBufferName},
		{name: "strategic reserve", failModule: reservetypes.StrategicReserveName},
		{name: "insurance", failModule: claimstypes.InsuranceName},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.clearTransientStore()
			policy := types.DefaultMonetaryPolicy()
			policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.34")
			policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.33")
			policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.33")
			s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
			s.setAssets(chain.USDBaseDenom)
			s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
				Return(sdk.NewInt64Coin(chain.USDBaseDenom, 0))
			s.bankKeeper.EXPECT().GetBalance(
				gomock.Any(),
				authtypes.NewModuleAddress(types.RedemptionBufferName),
				chain.NoahBaseDenom,
			).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0))
			credits := []struct {
				module string
				amount int64
			}{
				{types.RedemptionBufferName, 34},
				{reservetypes.StrategicReserveName, 33},
				{claimstypes.InsuranceName, 33},
			}
			for _, credit := range credits {
				call := s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					markettypes.ModuleName,
					credit.module,
					sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, credit.amount)),
				)
				if credit.module == test.failModule {
					call.Return(errors.New("injected transfer failure"))
					break
				}
				call.Return(nil)
			}

			_, err := s.keeper.RouteExpansion(
				s.ctx,
				sdk.NewInt64Coin(chain.NoahBaseDenom, 100),
				sdk.NewInt64Coin(chain.USDBaseDenom, 100),
				oracletypes.RateSet{
					chain.NoahBaseDenom: math.LegacyOneDec(),
					chain.USDBaseDenom:  math.LegacyOneDec(),
				},
			)
			s.Require().ErrorContains(err, "crediting "+test.failModule)
			s.Require().ErrorContains(err, "injected transfer failure")
		})
	}
}

func (s *KeeperTestSuite) TestDrawRedemptionBufferPaysCoverageShareOfOutput() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 40))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 8)),
	).Return(nil)

	bufferPaid, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 25),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyNewDec(2),
		},
	)
	s.Require().NoError(err)
	// Coverage 40/50 of a quoted 10 pays 8. The liability figures are Treasury's
	// own and never leave it, so they are restated here from the fixture: 100
	// ausd of supply at two per NOAH is 50, and 25 redeemed is 12.5.
	s.Require().Equal(math.NewInt(8), bufferPaid)
	aggregateLiability := math.LegacyNewDec(50)
	redeemedLiability := math.LegacyMustNewDecFromStr("12.5")
	bufferBefore := math.LegacyNewDec(40)
	bufferAfter := bufferBefore.Sub(math.LegacyNewDecFromInt(bufferPaid))
	liabilityAfter := aggregateLiability.Sub(redeemedLiability)
	s.Require().True(
		bufferAfter.Mul(aggregateLiability).GTE(bufferBefore.Mul(liabilityAfter)),
		"post-redemption Buffer coverage must not decrease",
	)

	s.requireTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:                      chain.NoahBaseDenom,
		Payment:                    math.NewInt(8),
		AggregateValuationComplete: true,
	})
}

func (s *KeeperTestSuite) TestDrawRedemptionBufferCoverageBoundariesAndTargetIndependence() {
	tests := []struct {
		name           string
		supply         int64
		redeemed       int64
		buffer         int64
		noahOutput     int64
		targetRatio    string
		expectedBuffer int64
	}{
		{
			name:           "zero buffer makes zero draw",
			supply:         100,
			redeemed:       25,
			buffer:         0,
			noahOutput:     20,
			targetRatio:    "0",
			expectedBuffer: 0,
		},
		{
			name:           "final liability is bounded by quoted output",
			supply:         25,
			redeemed:       25,
			buffer:         40,
			noahOutput:     20,
			targetRatio:    "0.5",
			expectedBuffer: 20,
		},
		{
			name:           "actual coverage with zero target ratio",
			supply:         100,
			redeemed:       25,
			buffer:         40,
			noahOutput:     20,
			targetRatio:    "0",
			expectedBuffer: 8,
		},
		{
			name:           "overfunded coverage is capped at one",
			supply:         3,
			redeemed:       1,
			buffer:         10,
			noahOutput:     1,
			targetRatio:    "0",
			expectedBuffer: 1,
		},
		{
			name:           "same actual coverage with full target ratio",
			supply:         100,
			redeemed:       25,
			buffer:         40,
			noahOutput:     20,
			targetRatio:    "1",
			expectedBuffer: 8,
		},
		{
			name:           "fractional covered output is floored",
			supply:         3,
			redeemed:       2,
			buffer:         1,
			noahOutput:     2,
			targetRatio:    "0",
			expectedBuffer: 0,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.clearTransientStore()
			policy := types.DefaultMonetaryPolicy()
			policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr(test.targetRatio)
			s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
			s.setAssets(chain.USDBaseDenom)
			s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
				Return(sdk.NewInt64Coin(chain.USDBaseDenom, test.supply))
			s.bankKeeper.EXPECT().GetBalance(
				gomock.Any(),
				authtypes.NewModuleAddress(types.RedemptionBufferName),
				chain.NoahBaseDenom,
			).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, test.buffer))
			if test.expectedBuffer > 0 {
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					types.RedemptionBufferName,
					markettypes.ModuleName,
					sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, test.expectedBuffer)),
				).Return(nil)
			}

			bufferPaid, err := s.keeper.DrawRedemptionBuffer(
				s.ctx,
				sdk.NewInt64Coin(chain.USDBaseDenom, test.redeemed),
				math.NewInt(test.noahOutput),
				oracletypes.RateSet{
					chain.NoahBaseDenom: math.LegacyOneDec(),
					chain.USDBaseDenom:  math.LegacyOneDec(),
				},
			)
			s.Require().NoError(err)
			s.Require().True(
				bufferPaid.Equal(math.NewInt(test.expectedBuffer)),
				"expected %d, got %s",
				test.expectedBuffer,
				bufferPaid,
			)
		})
	}
}

func (s *KeeperTestSuite) TestDrawRedemptionBufferRejectsOutputAboveRedeemedLiabilityBeforeAggregateValuation() {
	s.setAssets(chain.USDBaseDenom)

	_, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(11),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().ErrorContains(err, "NOAH output 11 exceeds redeemed liability 10")
}

// TestDrawRedemptionBufferFundsHealthyExitsDuringSuspension pins the contagion
// scenario the claimable denominator exists for: one member is suspended with
// no plan — untrusted, unable to redeem — while holders of the healthy member
// exit. The draw keeps funding those exits against the claimable aggregate,
// and at higher coverage than before the suspension (50/100 rather than
// 50/200), because the frozen supply is not competing for the Buffer. The
// suspension is disclosed through the incomplete flag, never a kill switch.
func (s *KeeperTestSuite) TestDrawRedemptionBufferFundsHealthyExitsDuringSuspension() {
	s.setAssets(chain.USDBaseDenom)
	s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100))
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	})
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 50))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 10)),
	).Return(nil)

	bufferPaid, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 25),
		math.NewInt(20),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	// Coverage 50/100 of the 20-NOAH output. With the suspended akrw counted
	// the denominator would be 200 and this would pay 5, so the payment is
	// what pins the claimable denominator; the event carries the incomplete
	// flag that used to travel back to Market.
	s.Require().Equal(math.NewInt(10), bufferPaid)
	s.requireTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:                      chain.NoahBaseDenom,
		Payment:                    math.NewInt(10),
		AggregateValuationComplete: false,
	})
}

// TestDrawRedemptionBufferDrawsForSuspendedDenomWithOpenPlan proves the
// settlement arm of the aggregate valuation: a suspended denomination under an
// open settlement plan is valued at the plan's committed rate rather than an
// oracle rate, and the plan need not have reached its activation height — the
// read is deliberately ungated so plan-open supply never disappears from the
// liability total between the block a plan opens and the block it binds.
func (s *KeeperTestSuite) TestDrawRedemptionBufferDrawsForSuspendedDenomWithOpenPlan() {
	s.setAssets()
	s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.plans[chain.USDBaseDenom] = usdSettlementPlan()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 40))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 2)),
	).Return(nil)

	// The quote rate matches the plan's 2-NOAH-per-unit commitment: half a
	// unit of ausd per NOAH.
	bufferPaid, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 25),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyMustNewDecFromStr("0.5"),
		},
	)
	s.Require().NoError(err)
	// 100 units at the plan's committed 2-NOAH rate makes the aggregate 200,
	// settlement-priced with no oracle read for the suspended denomination, and
	// coverage 40/200 of the 10-NOAH output floors to 2. An oracle-priced
	// aggregate would not have been 200, so the payment is what pins the
	// committed rate as the one used.
	s.Require().Equal(math.NewInt(2), bufferPaid)
	s.requireTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:                      chain.NoahBaseDenom,
		Payment:                    math.NewInt(2),
		AggregateValuationComplete: true,
	})
}

// usdSettlementPlan is a valid open plan whose activation height is still in
// the future relative to the suite's block height, so tests exercising it are
// also exercising the ungated plan read.
func usdSettlementPlan() assettypes.SettlementPlan {
	return assettypes.SettlementPlan{
		Denom:                 chain.USDBaseDenom,
		RedemptionRate:        math.LegacyNewDecWithPrec(5, 1),
		OpenedHeight:          1,
		ActivationHeight:      1_000,
		EarliestClosingHeight: 1100,
	}
}
