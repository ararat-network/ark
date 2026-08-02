package keeper_test

import (
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestSwapQuote_RecursiveSwap() {
	_, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: "1000ausd",
		AskDenom:  "ausd",
	})
	s.Require().Error(err)
	s.Require().ErrorContains(err, types.ErrRecursiveSwap.Error())
}

func (s *KeeperTestSuite) TestSwapQuote_StableToStable_TobinSpread() {
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.USDBaseDenom, chain.KRWBaseDenom).
		Return(oracletypes.RateSet{
			chain.USDBaseDenom: math.LegacyOneDec(),
			chain.KRWBaseDenom: math.LegacyNewDec(1300),
		}, nil).AnyTimes()

	offerCoin := sdk.NewCoin(chain.USDBaseDenom, math.NewInt(1_000_000))
	expectedGross := math.LegacyNewDec(1_300_000_000)
	tests := []struct {
		name           string
		offerOverride  math.LegacyDec
		askOverride    math.LegacyDec
		expectedSpread math.LegacyDec
	}{
		{
			// No entry on either leg is the ordinary case: an asset converts from
			// its first live block without governance choosing a rate for it.
			name:           "neither leg overridden charges the default",
			expectedSpread: types.DefaultTobinTax,
		},
		{
			name:           "an override on the offer leg widens the pair",
			offerOverride:  math.LegacyNewDecWithPrec(1, 2), // 1%
			expectedSpread: math.LegacyNewDecWithPrec(1, 2),
		},
		{
			name:           "an override on the ask leg widens the pair",
			askOverride:    math.LegacyNewDecWithPrec(50, 4), // 0.50%
			expectedSpread: math.LegacyNewDecWithPrec(50, 4),
		},
		{
			// The spread has to cover whichever leg carries more oracle-staleness
			// risk, so the wider rate governs both directions of the pair.
			name:           "the wider of two overrides governs",
			offerOverride:  math.LegacyNewDecWithPrec(3, 3),  // 0.30%
			askOverride:    math.LegacyNewDecWithPrec(15, 3), // 1.50%
			expectedSpread: math.LegacyNewDecWithPrec(15, 3),
		},
		{
			name:           "a zero leg cannot narrow the pair below its counterpart",
			offerOverride:  math.LegacyZeroDec(),
			expectedSpread: types.DefaultTobinTax,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Overrides are sparse and outlive a subtest, so each case starts from
			// an empty map rather than inheriting the previous case's entries.
			s.Require().NoError(s.keeper.TobinTaxOverrides.Clear(s.ctx, nil))
			if !tc.offerOverride.IsNil() {
				s.seedTobinTaxOverride(chain.USDBaseDenom, tc.offerOverride)
			}
			if !tc.askOverride.IsNil() {
				s.seedTobinTaxOverride(chain.KRWBaseDenom, tc.askOverride)
			}

			response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
				OfferCoin: offerCoin.String(),
				AskDenom:  chain.KRWBaseDenom,
			})
			s.Require().NoError(err)
			s.Require().Equal(chain.KRWBaseDenom, response.SwapCoin.Denom)
			expectedFee := tc.expectedSpread.Mul(expectedGross)
			s.Require().True(expectedFee.Equal(response.SwapFee.Amount),
				"expected fee %s, got %s", expectedFee, response.SwapFee.Amount)
		})
	}
}

// TestSwapQuoteEligibility walks the lifecycle matrix on both legs of both pair
// shapes. Simulation shares the gate with execution, so a quote must refuse
// exactly what a swap would refuse.
func (s *KeeperTestSuite) TestSwapQuoteEligibility() {
	rates := oracletypes.RateSet{
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.KRWBaseDenom:  math.LegacyNewDec(1300),
		chain.SDRBaseDenom:  math.LegacyOneDec(),
		chain.NoahBaseDenom: math.LegacyOneDec(),
	}
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(rates, nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(rates, nil).AnyTimes()

	statuses := []struct {
		status    assettypes.AssetStatus
		offerable bool
		askable   bool
	}{
		{
			status:    assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
			offerable: true,
			askable:   true,
		},
		{
			// Halting issuance preserves every exit and forbids every entry, so
			// the same status passes as an offer and fails as an ask.
			status:    assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			offerable: true,
		},
		{status: assettypes.AssetStatus_ASSET_STATUS_SUSPENDED},
		{status: assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF},
		{status: assettypes.AssetStatus_ASSET_STATUS_RETIRED},
		{status: assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED},
	}

	// Every pair bends the same denomination, ausd, so the row names say which
	// leg it occupies and the NOAH pairs cover the case where only one leg has a
	// registry entry at all.
	pairs := []struct {
		name      string
		offerCoin sdk.Coin
		askDenom  string
		asksUSD   bool
	}{
		{
			name:      "stable pair offer leg",
			offerCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
			askDenom:  chain.KRWBaseDenom,
		},
		{
			name:      "stable pair ask leg",
			offerCoin: sdk.NewInt64Coin(chain.KRWBaseDenom, 1_000_000),
			askDenom:  chain.USDBaseDenom,
			asksUSD:   true,
		},
		{
			name:      "noah pair offer leg",
			offerCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
			askDenom:  chain.NoahBaseDenom,
		},
		{
			name:      "noah pair ask leg",
			offerCoin: sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000),
			askDenom:  chain.USDBaseDenom,
			asksUSD:   true,
		},
	}

	for _, pair := range pairs {
		for _, tc := range statuses {
			s.Run(pair.name+" "+tc.status.String(), func() {
				s.assetStatuses = map[string]assettypes.AssetStatus{
					chain.USDBaseDenom: tc.status,
				}

				response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
					OfferCoin: pair.offerCoin.String(),
					AskDenom:  pair.askDenom,
				})

				allowed := tc.offerable
				rejection := "cannot be offered for conversion"
				if pair.asksUSD {
					allowed = tc.askable
					rejection = "cannot be produced by conversion"
				}
				if allowed {
					s.Require().NoError(err)
					s.Require().Equal(pair.askDenom, response.SwapCoin.Denom)
					return
				}

				// A refused leg is a precondition failure, not an internal one: the
				// request is well formed and the chain simply will not convert that
				// denomination in that direction right now.
				s.Require().Error(err)
				s.Require().Equal(codes.FailedPrecondition, status.Code(err))
				if tc.status == assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED {
					s.Require().ErrorContains(err, assettypes.ErrAssetNotFound.Error())
					return
				}
				s.Require().ErrorContains(err, types.ErrIneligibleAsset.Error())
				s.Require().ErrorContains(err, rejection)
			})
		}
	}
}

func (s *KeeperTestSuite) TestSwapQuoteNeverGatesNoah() {
	// NOAH is the numeraire and has no registry entry, so nothing about the
	// lifecycle can make it unconvertible. A status planted under its
	// denomination proves the gate never asks about it.
	s.assetStatuses[chain.NoahBaseDenom] = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.USDBaseDenom,
		chain.SDRBaseDenom,
		chain.NoahBaseDenom,
	).Return(oracletypes.RateSet{
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.SDRBaseDenom:  math.LegacyOneDec(),
		chain.NoahBaseDenom: math.LegacyOneDec(),
	}, nil)

	response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000).String(),
		AskDenom:  chain.NoahBaseDenom,
	})
	s.Require().NoError(err)
	s.Require().Equal(chain.NoahBaseDenom, response.SwapCoin.Denom)
}

func (s *KeeperTestSuite) TestMsgSwapRejectsIneligibleAssetBeforeReadingRates() {
	trader := sdk.AccAddress([]byte("trader_______________"))
	s.assetStatuses[chain.USDBaseDenom] = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED

	// No rate expectation is registered: eligibility precedes every rate read, so
	// a suspended offer must fail before the oracle is consulted and before any
	// coin moves. The strict mocks fail this test if either happens.
	_, err := s.msgServer.Swap(s.ctx, &types.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
		AskDenom:       chain.KRWBaseDenom,
		MinimumReceive: sdk.NewInt64Coin(chain.KRWBaseDenom, 1),
	})
	s.Require().ErrorIs(err, types.ErrIneligibleAsset)
	s.Require().ErrorContains(err, "cannot be offered for conversion")
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestSwapQuote_ConstantProduct() {
	// Unit rates (1:1:1) with a small base pool so CP spread is significant.
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(oracletypes.RateSet{
			"ausd":              math.LegacyOneDec(),
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.SDRBaseDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()

	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = sdrBasePool(math.LegacyNewDec(400))
	capacity.MinStabilitySpread = math.LegacyNewDecWithPrec(2, 2) // 2%
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))

	tests := []struct {
		name           string
		offerCoin      sdk.Coin
		askDenom       string
		expectedDenom  string
		expectedAmount math.LegacyDec
		expectedSpread math.LegacyDec
	}{
		{
			name:           "ark to noah — CP spread = 100/500 = 0.2",
			offerCoin:      sdk.NewCoin("ausd", math.NewInt(100)),
			askDenom:       chain.NoahBaseDenom,
			expectedDenom:  chain.NoahBaseDenom,
			expectedAmount: math.LegacyNewDec(100),
			expectedSpread: math.LegacyNewDecWithPrec(2, 1), // 0.2
		},
		{
			name:           "noah to ark — symmetric with balanced pools",
			offerCoin:      sdk.NewCoin(chain.NoahBaseDenom, math.NewInt(100)),
			askDenom:       "ausd",
			expectedDenom:  "ausd",
			expectedAmount: math.LegacyNewDec(100),
			expectedSpread: math.LegacyNewDecWithPrec(2, 1), // 0.2
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
				OfferCoin: tc.offerCoin.String(),
				AskDenom:  tc.askDenom,
			})
			s.Require().NoError(err)
			s.Require().Equal(tc.expectedDenom, response.SwapCoin.Denom)
			expectedFee := tc.expectedSpread.Mul(tc.expectedAmount)
			s.Require().True(expectedFee.Equal(response.SwapFee.Amount),
				"expected fee %s, got %s", expectedFee, response.SwapFee.Amount)
			grossAmount := math.LegacyNewDecFromInt(response.SwapCoin.Amount).Add(response.SwapFee.Amount)
			s.Require().True(tc.expectedAmount.Equal(grossAmount),
				"expected gross amount %s, got %s", tc.expectedAmount, grossAmount)
		})
	}
}

func (s *KeeperTestSuite) TestSwapQuote_SpreadNeverBelowMinSpread() {
	// With small offers into the default large pool (1e12), CP spread ≈ 0.
	// The minimum stability spread (2%) should always be the floor.
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), "ausd", chain.SDRBaseDenom, chain.NoahBaseDenom).
		Return(oracletypes.RateSet{
			"ausd":              math.LegacyOneDec(),
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.SDRBaseDenom:  math.LegacyOneDec(),
		}, nil).AnyTimes()

	minSpread := math.LegacyNewDecWithPrec(2, 2) // 2%

	for _, amt := range []int64{10, 100, 1000, 10000} {
		offerCoin := sdk.NewCoin("ausd", math.NewInt(amt))
		response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
			OfferCoin: offerCoin.String(),
			AskDenom:  chain.NoahBaseDenom,
		})
		s.Require().NoError(err)
		spread := response.SwapFee.Amount.Quo(math.LegacyNewDec(amt))
		s.Require().True(spread.GTE(minSpread),
			"spread %s below minSpread %s for amount %d", spread, minSpread, amt)
	}
}

func (s *KeeperTestSuite) TestSwapQuote_NegativeRawSpreadUsesMinimumSpread() {
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		"ausd",
		chain.SDRBaseDenom,
		chain.NoahBaseDenom,
	).Return(oracletypes.RateSet{
		"ausd":              math.LegacyOneDec(),
		chain.SDRBaseDenom:  math.LegacyOneDec(),
		chain.NoahBaseDenom: math.LegacyOneDec(),
	}, nil)
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(
		s.ctx,
		math.LegacyMustNewDecFromStr("-500000000000"),
	))

	offerCoin := sdk.NewInt64Coin("ausd", 100_000_000_000)
	response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  chain.NoahBaseDenom,
	})
	s.Require().NoError(err)
	spread := response.SwapFee.Amount.Quo(math.LegacyNewDecFromInt(offerCoin.Amount))
	s.Require().True(types.DefaultMinStabilitySpread.Equal(spread))
}

func (s *KeeperTestSuite) TestSwapQuote_ExtremeNegativeRawSpreadUsesMinimumWithoutOverflow() {
	basePool := math.LegacyMustNewDecFromStr("100000000000000000000000000000")
	arkPoolDelta := basePool.Neg().Add(math.LegacySmallestDec())
	_, err := types.NewEffectivePools(basePool, arkPoolDelta)
	s.Require().NoError(err)

	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = sdrBasePool(basePool)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, arkPoolDelta))

	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.USDBaseDenom,
		chain.SDRBaseDenom,
		chain.NoahBaseDenom,
	).Return(oracletypes.RateSet{
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.SDRBaseDenom:  math.LegacySmallestDec(),
		chain.NoahBaseDenom: math.LegacyOneDec(),
	}, nil)

	response, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000).String(),
		AskDenom:  chain.NoahBaseDenom,
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 980_000), response.SwapCoin)
	s.Require().True(math.LegacyNewDec(20_000).Equal(response.SwapFee.Amount))
}

// TestSwapQuoteRefusesSwapsThatRoundToZero covers the two ways a NOAH-pair
// quote can be left with nothing to pay out.
//
// The first is the boundary that bounds the spread at all: an offer too small
// to move the pool by one representable unit leaves the constant product
// returning no ask amount, so the whole offer is charged as spread. A spread
// above one would drive the payout negative rather than to zero, which is why
// it cannot happen — both divisions round to nearest at the same precision, and
// the smallest representable offer is twice the largest rounding residue, so
// the ask amount is never negative and the spread never exceeds one. Exactly
// one is reachable, and it must refuse rather than pay zero.
//
// The second is the ordinary one-unit swap, where a spread well under one still
// truncates the entire payout away.
func (s *KeeperTestSuite) TestSwapQuoteRefusesSwapsThatRoundToZero() {
	// The base-pool leg prices at the smallest representable rate, so one unit
	// offered converts to 1e-18 base-pool units — below the pool's own smallest
	// movement once the ark side sits at twice the depth.
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(oracletypes.RateSet{
			chain.USDBaseDenom:  math.LegacyOneDec(),
			chain.SDRBaseDenom:  math.LegacySmallestDec(),
			chain.NoahBaseDenom: math.LegacyOneDec(),
		}, nil).AnyTimes()

	basePool := math.LegacyMustNewDecFromStr("1000000000000")
	capacity := types.DefaultConversionPolicy()
	capacity.BasePool = sdrBasePool(basePool)
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, capacity))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, basePool))

	tests := []struct {
		name      string
		offerCoin sdk.Coin
		askDenom  string
	}{
		{
			name:      "an offer the pool cannot register is charged entirely as spread",
			offerCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 1),
			askDenom:  chain.NoahBaseDenom,
		},
		{
			name:      "one unit truncates away under the minimum spread",
			offerCoin: sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
			askDenom:  chain.USDBaseDenom,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
				OfferCoin: tc.offerCoin.String(),
				AskDenom:  tc.askDenom,
			})
			s.Require().Error(err)
			s.Require().Equal(codes.InvalidArgument, status.Code(err))
			s.Require().ErrorContains(err, types.ErrZeroSwapCoin.Error())
		})
	}
}

func (s *KeeperTestSuite) TestSwapQuote_PoolImbalanceIncreasesSpread() {
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), "ausd", chain.SDRBaseDenom, chain.NoahBaseDenom).
		Return(oracletypes.RateSet{
			"ausd":              math.LegacyOneDec(),
			chain.NoahBaseDenom: math.LegacyNewDecWithPrec(5, 1),
			chain.SDRBaseDenom:  math.LegacyNewDecWithPrec(17, 1),
		}, nil).AnyTimes()

	offerCoin := sdk.NewCoin(chain.USDBaseDenom, chain.NativeBaseAmount(1))

	// Get spread with balanced pool
	balancedQuote, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  chain.NoahBaseDenom,
	})
	s.Require().NoError(err)

	// Set large pool delta (imbalanced)
	err = s.keeper.ArkPoolDelta.Set(
		s.ctx,
		math.LegacyNewDecFromInt(chain.NativeBaseAmount(1_000_000)),
	)
	s.Require().NoError(err)

	// Spread should be larger with imbalanced pool
	imbalancedQuote, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  chain.NoahBaseDenom,
	})
	s.Require().NoError(err)
	s.Require().True(imbalancedQuote.SwapFee.Amount.GT(balancedQuote.SwapFee.Amount))
}
