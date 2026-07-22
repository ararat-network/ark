package keeper_test

import (
	"errors"
	"math/big"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestRouteExpansionRejectsInvalidInputs() {
	tests := []struct {
		name          string
		grossOffer    sdk.Coin
		stableOutput  sdk.Coin
		wantErr       string
		needsRegistry bool
	}{
		{
			name:         "wrong gross denom",
			grossOffer:   sdk.NewInt64Coin(chain.MicroUSDDenom, 1),
			stableOutput: sdk.NewInt64Coin(chain.MicroUSDDenom, 1),
			wantErr:      "invalid gross offer",
		},
		{
			name:         "zero gross offer",
			grossOffer:   sdk.NewInt64Coin(chain.MicroNoahDenom, 0),
			stableOutput: sdk.NewInt64Coin(chain.MicroUSDDenom, 1),
			wantErr:      "invalid gross offer",
		},
		{
			name:         "negative gross offer",
			grossOffer:   sdk.Coin{Denom: chain.MicroNoahDenom, Amount: math.NewInt(-1)},
			stableOutput: sdk.NewInt64Coin(chain.MicroUSDDenom, 1),
			wantErr:      "invalid gross offer",
		},
		{
			name:         "zero stable output",
			grossOffer:   sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
			stableOutput: sdk.NewInt64Coin(chain.MicroUSDDenom, 0),
			wantErr:      "stable output must be positive",
		},
		{
			name:         "malformed stable output",
			grossOffer:   sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
			stableOutput: sdk.Coin{Denom: "BAD DENOM", Amount: math.OneInt()},
			wantErr:      "invalid stable output",
		},
		{
			name:          "unconfigured stable output",
			grossOffer:    sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
			stableOutput:  sdk.NewInt64Coin("uatom", 1),
			wantErr:       "is not configured in oracle",
			needsRegistry: true,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			if test.needsRegistry {
				s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
					{Denom: chain.MicroUSDDenom},
				}, nil)
			}
			_, err := s.keeper.RouteExpansion(
				s.ctx,
				test.grossOffer,
				test.stableOutput,
				oracletypes.RateSnapshot{},
			)
			s.Require().ErrorContains(err, test.wantErr)
		})
	}
}

func (s *KeeperTestSuite) TestRouteExpansionFailsBeforeTransferWhenOutputRateUnavailable() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
	}, nil)

	_, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
		sdk.NewInt64Coin(chain.MicroUSDDenom, 60),
		oracletypes.RateSnapshot{chain.MicroNoahDenom: math.LegacyOneDec()},
	)
	s.Require().ErrorIs(err, oracletypes.ErrUnknownDenom)
}

func (s *KeeperTestSuite) TestRouteExpansionRejectsOutputValueAboveGrossOffer() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
	}, nil)

	_, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
		sdk.NewInt64Coin(chain.MicroUSDDenom, 101),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().ErrorContains(err, "stable output value 101unoah exceeds gross offer 100unoah")
}

func (s *KeeperTestSuite) TestRouteExpansionSkipsZeroCredits() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroKRWDenom},
	}, nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroKRWDenom).
		Return(sdk.NewInt64Coin(chain.MicroKRWDenom, 10))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 0))
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), chain.MicroKRWDenom).
		Return(oracletypes.RateSnapshot{chain.MicroKRWDenom: math.LegacyOneDec()}, nil)
	for _, moduleName := range []string{
		types.RedemptionBufferName,
		types.StrategicReserveName,
		types.InsuranceName,
	} {
		s.bankKeeper.EXPECT().GetBalance(
			gomock.Any(),
			authtypes.NewModuleAddress(moduleName),
			chain.MicroNoahDenom,
		).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 0))
	}

	quoteRates := oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom:  math.LegacyOneDec(),
	}
	allocation, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
		sdk.NewInt64Coin(chain.MicroUSDDenom, 60),
		quoteRates,
	)
	s.Require().NoError(err)
	s.Require().True(allocation.RedemptionBufferCredit.IsZero())
	s.Require().True(allocation.StrategicReserveCredit.IsZero())
	s.Require().True(allocation.InsuranceCredit.IsZero())
	s.Require().Equal(allocation.EligiblePrincipalNoah, allocation.OverflowBurn)
}

func (s *KeeperTestSuite) TestRouteExpansionUsesTargetWaterfall() {
	policy := types.DefaultMonetaryPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
	}, nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 40))
	for _, moduleName := range []string{
		types.RedemptionBufferName,
		types.StrategicReserveName,
		types.InsuranceName,
	} {
		s.bankKeeper.EXPECT().GetBalance(
			gomock.Any(),
			authtypes.NewModuleAddress(moduleName),
			chain.MicroNoahDenom,
		).
			Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 0))
	}
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 50)),
	).Return(nil)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.StrategicReserveName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 10)),
	).Return(nil)

	allocation, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
		sdk.NewInt64Coin(chain.MicroUSDDenom, 60),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(60), allocation.EligiblePrincipalNoah)
	s.Require().Equal(math.NewInt(50), allocation.RedemptionBufferCredit)
	s.Require().Equal(math.NewInt(10), allocation.StrategicReserveCredit)
	s.Require().True(allocation.InsuranceCredit.IsZero())
	s.Require().Equal(math.NewInt(40), allocation.SpreadAndDustBurn)
	s.Require().True(allocation.OverflowBurn.IsZero())
	s.Require().True(allocation.TargetValuationComplete)
	s.requireTypedEvent(&types.EventExpansionAllocated{
		Denom:                   chain.MicroNoahDenom,
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
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
	}, nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 0))
	for _, moduleName := range []string{
		types.RedemptionBufferName,
		types.StrategicReserveName,
		types.InsuranceName,
	} {
		s.bankKeeper.EXPECT().GetBalance(
			gomock.Any(),
			authtypes.NewModuleAddress(moduleName),
			chain.MicroNoahDenom,
		).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 0))
	}
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 2)),
	).Return(nil)

	allocation, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroNoahDenom, 20),
		sdk.NewInt64Coin(chain.MicroUSDDenom, 101),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyNewDec(10),
		},
	)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(10), allocation.EligiblePrincipalNoah)
	s.Require().Equal(math.NewInt(2), allocation.RedemptionBufferCredit)
	s.Require().Equal(math.NewInt(8), allocation.OverflowBurn)
	s.Require().Equal(math.NewInt(10), allocation.SpreadAndDustBurn)
}

func (s *KeeperTestSuite) TestRouteExpansionFallsBackToBufferOnUnrelatedStaleRate() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroKRWDenom},
	}, nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroKRWDenom).
		Return(sdk.NewInt64Coin(chain.MicroKRWDenom, 10))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 0))
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), chain.MicroKRWDenom).
		Return(nil, oracletypes.ErrStaleExchangeRate)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 60)),
	).Return(nil)

	allocation, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
		sdk.NewInt64Coin(chain.MicroUSDDenom, 60),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(allocation.TargetValuationComplete)
	s.Require().Equal(allocation.EligiblePrincipalNoah, allocation.RedemptionBufferCredit)
	s.Require().True(allocation.StrategicReserveCredit.IsZero())
	s.Require().True(allocation.InsuranceCredit.IsZero())
	s.Require().True(allocation.OverflowBurn.IsZero())
}

func (s *KeeperTestSuite) TestRouteExpansionFallsBackToBufferOnAggregateOverflow() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroKRWDenom},
	}, nil)
	largeSupply := new(big.Int).Lsh(big.NewInt(1), uint(math.MaxBitLen-1))
	largeSupply.Add(largeSupply, big.NewInt(1))
	for _, denom := range []string{chain.MicroKRWDenom, chain.MicroUSDDenom} {
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), denom).
			Return(sdk.NewCoin(denom, math.NewIntFromBigInt(largeSupply)))
	}
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), markettypes.ModuleName, types.RedemptionBufferName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1)),
	).Return(nil)

	allocation, err := s.keeper.RouteExpansion(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
		sdk.NewInt64Coin(chain.MicroUSDDenom, 1),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyOneDec(),
			chain.MicroKRWDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(allocation.TargetValuationComplete)
	s.Require().Equal(math.OneInt(), allocation.RedemptionBufferCredit)
}

func (s *KeeperTestSuite) TestRouteExpansionPropagatesEachFixedTransferFailure() {
	tests := []struct {
		name       string
		failModule string
	}{
		{name: "redemption buffer", failModule: types.RedemptionBufferName},
		{name: "strategic reserve", failModule: types.StrategicReserveName},
		{name: "insurance", failModule: types.InsuranceName},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.clearTransientStore()
			policy := types.DefaultMonetaryPolicy()
			policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.34")
			policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.33")
			policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.33")
			s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
			s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
				{Denom: chain.MicroUSDDenom},
			}, nil)
			s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
				Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 0))
			for _, moduleName := range []string{
				types.RedemptionBufferName,
				types.StrategicReserveName,
				types.InsuranceName,
			} {
				s.bankKeeper.EXPECT().GetBalance(
					gomock.Any(),
					authtypes.NewModuleAddress(moduleName),
					chain.MicroNoahDenom,
				).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 0))
			}
			credits := []struct {
				module string
				amount int64
			}{
				{types.RedemptionBufferName, 34},
				{types.StrategicReserveName, 33},
				{types.InsuranceName, 33},
			}
			for _, credit := range credits {
				call := s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					markettypes.ModuleName,
					credit.module,
					sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, credit.amount)),
				)
				if credit.module == test.failModule {
					call.Return(errors.New("injected transfer failure"))
					break
				}
				call.Return(nil)
			}

			_, err := s.keeper.RouteExpansion(
				s.ctx,
				sdk.NewInt64Coin(chain.MicroNoahDenom, 100),
				sdk.NewInt64Coin(chain.MicroUSDDenom, 100),
				oracletypes.RateSnapshot{
					chain.MicroNoahDenom: math.LegacyOneDec(),
					chain.MicroUSDDenom:  math.LegacyOneDec(),
				},
			)
			s.Require().ErrorContains(err, "crediting "+test.failModule)
			s.Require().ErrorContains(err, "injected transfer failure")
		})
	}
}

func (s *KeeperTestSuite) TestDrawRedemptionBufferPaysCoverageShareOfOutput() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
	}, nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 100))
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.MicroNoahDenom).
		Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 40))
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 8)),
	).Return(nil)

	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 25),
		math.NewInt(10),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyNewDec(2),
		},
	)
	s.Require().NoError(err)
	s.Require().True(draw.ValuationComplete)
	s.Require().Equal(math.LegacyMustNewDecFromStr("12.5"), draw.RedeemedLiabilityNoah)
	s.Require().Equal(math.LegacyNewDec(50), draw.AggregateLiabilityNoah)
	s.Require().Equal(math.NewInt(8), draw.BufferPaid)
	bufferBefore := math.LegacyNewDec(40)
	bufferAfter := math.LegacyNewDec(32)
	liabilityAfter := draw.AggregateLiabilityNoah.Sub(draw.RedeemedLiabilityNoah)
	s.Require().True(
		bufferAfter.Mul(draw.AggregateLiabilityNoah).GTE(bufferBefore.Mul(liabilityAfter)),
		"post-redemption Buffer coverage must not decrease",
	)

	s.requireTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:                      chain.MicroNoahDenom,
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
			s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
				{Denom: chain.MicroUSDDenom},
			}, nil)
			s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
				Return(sdk.NewInt64Coin(chain.MicroUSDDenom, test.supply))
			s.bankKeeper.EXPECT().GetBalance(
				gomock.Any(),
				authtypes.NewModuleAddress(types.RedemptionBufferName),
				chain.MicroNoahDenom,
			).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, test.buffer))
			if test.expectedBuffer > 0 {
				s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					types.RedemptionBufferName,
					markettypes.ModuleName,
					sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, test.expectedBuffer)),
				).Return(nil)
			}

			draw, err := s.keeper.DrawRedemptionBuffer(
				s.ctx,
				sdk.NewInt64Coin(chain.MicroUSDDenom, test.redeemed),
				math.NewInt(test.noahOutput),
				oracletypes.RateSnapshot{
					chain.MicroNoahDenom: math.LegacyOneDec(),
					chain.MicroUSDDenom:  math.LegacyOneDec(),
				},
			)
			s.Require().NoError(err)
			s.Require().True(draw.ValuationComplete)
			s.Require().True(
				draw.BufferPaid.Equal(math.NewInt(test.expectedBuffer)),
				"expected %d, got %s",
				test.expectedBuffer,
				draw.BufferPaid,
			)
		})
	}
}

func (s *KeeperTestSuite) TestDrawRedemptionBufferRejectsOutputAboveRedeemedLiabilityBeforeAggregateValuation() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
	}, nil)

	_, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 10),
		math.NewInt(11),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().ErrorContains(err, "NOAH output 11 exceeds redeemed liability 10")
}

func (s *KeeperTestSuite) TestDrawRedemptionBufferFallsBackOnIncompleteAggregateValuation() {
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroKRWDenom},
	}, nil)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 100))
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroKRWDenom).
		Return(sdk.NewInt64Coin(chain.MicroKRWDenom, 100))
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(), chain.MicroKRWDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)

	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 25),
		math.NewInt(20),
		oracletypes.RateSnapshot{
			chain.MicroNoahDenom: math.LegacyOneDec(),
			chain.MicroUSDDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(draw.ValuationComplete)
	s.Require().True(draw.AggregateLiabilityNoah.IsZero())
	s.Require().True(draw.BufferPaid.IsZero())
}
