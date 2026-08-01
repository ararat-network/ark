package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/keeper"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestLiabilitySnapshotReusesScanAndTracksSupplyChanges() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		gomock.Any(),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.KRWBaseDenom:  math.LegacyOneDec(),
	}
	first, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(200), first.AggregateLiabilityNoah)

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 20),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 10),
		rates,
	))

	second, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(190), second.AggregateLiabilityNoah)
}

func (s *KeeperTestSuite) TestLiabilityIncompleteValuationMarksBlockUnavailable() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	// akrw has no rate this block, so the priced bucket cannot be completed.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	for range 2 {
		draw, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		s.Require().False(draw.ValuationComplete)
		s.Require().True(draw.BufferPaid.IsZero())
	}

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
		rates,
	))

	// Key 0x01 holds the block's valuation, and 0x00 is the unavailable
	// sentinel (mirrors the unexported values in liability.go).
	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	valuation, err := transientStore.Get([]byte{0x01})
	s.Require().NoError(err)
	s.Require().Equal([]byte{0x00}, valuation)
}

// The unavailable sentinel shares a key with marshalled valuations, so it must
// never be a value LegacyDec.Marshal can produce.
func (s *KeeperTestSuite) TestLiabilityUnavailableSentinelCannotCollide() {
	for _, liability := range []math.LegacyDec{
		math.LegacyZeroDec(),
		math.LegacyOneDec(),
		math.LegacyNewDec(200),
		math.LegacyNewDecWithPrec(1, 18),
		math.LegacyMustNewDecFromStr("115792089237316195423570985008687907853269984665640564039457584007913129639935"),
	} {
		encoded, err := liability.Marshal()
		s.Require().NoError(err)
		s.Require().NotEmpty(encoded)
		s.Require().NotEqual([]byte{0x00}, encoded)
		s.Require().NotContains(encoded, byte(0x00))
	}
}

func (s *KeeperTestSuite) TestLiabilitySnapshotResetsAtBlockCommit() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(2)
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		gomock.Any(),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	for block := int64(1); block <= 2; block++ {
		draw, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		s.Require().Equal(math.LegacyNewDec(100), draw.AggregateLiabilityNoah)

		if block == 1 {
			s.commitMultiStore.Commit()
			s.setBlockHeight(block + 1)
		}
	}
}

func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotStoresCompleteValuation() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	})
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(1)

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// GetSupply/GetRateSet expectations are exhausted by priming: the draw
	// below must reuse the primed snapshot without rescanning.
	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().True(draw.ValuationComplete)
	s.Require().Equal(math.LegacyNewDec(200), draw.AggregateLiabilityNoah)
}

// TestPrimeLiabilitySnapshotRecognizesSettlementPricedSupply proves the
// preblock prime carries suspended supply at its settlement plan's committed
// rate — including a plan that has not reached its activation height, because
// the plan read is deliberately ungated — so transaction-time draws find a
// complete snapshot without any oracle rate for the suspended denomination.
func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotRecognizesSettlementPricedSupply() {
	s.setAssets()
	s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.plans[chain.USDBaseDenom] = usdSettlementPlan()
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(1)

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// 100 units at the committed 2-NOAH rate; the exhausted GetSupply
	// expectation proves the draw reuses the primed snapshot.
	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyMustNewDecFromStr("0.5"),
		},
	)
	s.Require().NoError(err)
	s.Require().True(draw.ValuationComplete)
	s.Require().Equal(math.LegacyNewDec(200), draw.AggregateLiabilityNoah)
}

// TestPrimeLiabilitySnapshotCountsUnderflowingDustSupplyAsZero pins
// measurement semantics for the priced partition: an outstanding supply whose
// NOAH value truncates below Dec precision counts as zero instead of marking
// the whole aggregate unavailable. One hyperinflated denomination's dust must
// not disable expansion routing and Buffer draws chain-wide.
func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotCountsUnderflowingDustSupplyAsZero() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 1)).Times(1)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyNewDec(10).Power(19),
	})
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(1)

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().True(draw.ValuationComplete)
	s.Require().Equal(math.LegacyNewDec(100), draw.AggregateLiabilityNoah)
}

// TestPrimeLiabilitySnapshotDegradesOnUnrepresentableConversion pins the
// per-coin representability exit: a member the partition prices, whose supply
// at its rate leaves Dec range, is the per-coin form of the aggregate overflow
// and takes the same exit — the block degrades to the conservative fallback
// instead of failing preblock. Capture-layer failures cannot reach that
// conversion (both rates exist by construction), so out-of-range is the only
// error class with a degrade path at that site.
func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotDegradesOnUnrepresentableConversion() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewCoin(chain.USDBaseDenom, math.NewIntWithDecimal(1, 60))).Times(1)
	// At 1e-18 ausd per NOAH, 1e60 base units value to 1e78 NOAH — beyond
	// LegacyDec range.
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(1, 18),
	})

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(draw.ValuationComplete)
	s.Require().True(draw.BufferPaid.IsZero())
}

func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotMarksUnavailableValuation() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.setRates(oracletypes.RateSet{})

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// The rest of the block reuses the marker without rescanning.
	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(draw.ValuationComplete)
	s.Require().True(draw.BufferPaid.IsZero())
}

func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotOverridesEarlierPrime() {
	s.setAssets(chain.USDBaseDenom)
	gomock.InOrder(
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
			Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1),
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
			Return(sdk.NewInt64Coin(chain.USDBaseDenom, 200)).Times(1),
	)
	// A complete prime stores a snapshot; a later prime that cannot value the
	// same denom must retract it rather than leave the stale value winning.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))
	s.setRates(oracletypes.RateSet{})
	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(draw.ValuationComplete)
	s.Require().True(draw.BufferPaid.IsZero())
}

func (s *KeeperTestSuite) TestLiabilityValuationGasIsPositionIndependent() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	// Quote rates cover every listed denom, so the lazy scan never calls
	// GetRateSet and the map is never mutated; sharing it across draws is safe.
	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.KRWBaseDenom:  math.LegacyOneDec(),
	}
	draw := func() uint64 {
		before := sdk.UnwrapSDKContext(s.ctx).GasMeter().GasConsumed()
		_, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		return sdk.UnwrapSDKContext(s.ctx).GasMeter().GasConsumed() - before
	}

	scanGas := draw() // first call performs the full lazy scan
	hitGas := draw()  // second call reads the snapshot
	s.Require().Equal(scanGas, hitGas)
	// 2_000 mirrors liabilityValuationGas in liability.go.
	s.Require().GreaterOrEqual(hitGas, uint64(2_000))
}

func (s *KeeperTestSuite) TestRecordSupplyChangeWithoutSnapshotIsNoOp() {
	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 9),
		oracletypes.RateSet{
			chain.USDBaseDenom: math.LegacyOneDec(),
			chain.KRWBaseDenom: math.LegacyOneDec(),
		},
	))

	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	iterator, err := transientStore.Iterator(nil, nil)
	s.Require().NoError(err)
	s.Require().False(iterator.Valid())
	s.Require().NoError(iterator.Close())
}

// TestFundStatusPartitionsLiabilityByLifecycleStatus drives the liability
// partition through every lifecycle bucket via the query that discloses it.
// The invariants under test: priced and settlement-priced supply is
// recognised; untrusted suspended exposure is recognised but unvaluable, so
// it alone makes the total unavailable; written-off supply is disclosed
// without affecting recognition or availability; PENDING and RETIRED supply
// is invisible; and a rate failure zeroes the priced bucket while the
// disclosure lists survive, because they never depend on rates.
func (s *KeeperTestSuite) TestFundStatusPartitionsLiabilityByLifecycleStatus() {
	writeOff := func(denom string, amount int64) types.WrittenOffExposure {
		return types.WrittenOffExposure{
			OutstandingSupply: sdk.NewInt64Coin(denom, amount),
			WriteOffVersion:   1,
		}
	}
	tests := []struct {
		name           string
		seed           func()
		supplies       map[string]int64
		expectRates    func()
		wantPriced     string
		wantSettlement string
		wantUntrusted  []sdk.Coin
		wantWrittenOff []types.WrittenOffExposure
		wantAvailable  bool
	}{
		{
			name: "all active supply is oracle-priced",
			seed: func() {
				s.setAssets(chain.KRWBaseDenom, chain.USDBaseDenom)
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 100, chain.USDBaseDenom: 100},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{
					chain.KRWBaseDenom: math.LegacyNewDec(2),
					chain.USDBaseDenom: math.LegacyOneDec(),
				})
			},
			wantPriced:    "150",
			wantAvailable: true,
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
			wantPriced:    "100",
			wantAvailable: true,
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
			wantAvailable:  true,
		},
		{
			name: "suspended supply without a plan is untrusted exposure",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
			},
			supplies:      map[string]int64{chain.USDBaseDenom: 100},
			wantUntrusted: []sdk.Coin{sdk.NewInt64Coin(chain.USDBaseDenom, 100)},
			wantAvailable: false,
		},
		{
			name: "written-off supply is disclosed but stays derecognized",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
			},
			supplies:       map[string]int64{chain.USDBaseDenom: 60},
			wantWrittenOff: []types.WrittenOffExposure{writeOff(chain.USDBaseDenom, 60)},
			wantAvailable:  true,
		},
		{
			// Positive PENDING or RETIRED supply cannot exist in practice;
			// if it ever does, it must not leak into any bucket.
			name: "pending and retired supply is invisible",
			seed: func() {
				s.setAssets()
				s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_PENDING)
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_RETIRED)
			},
			supplies:      map[string]int64{chain.KRWBaseDenom: 50, chain.USDBaseDenom: 70},
			wantAvailable: true,
		},
		{
			name: "zero supply is skipped entirely",
			seed: func() {
				s.setAssets(chain.USDBaseDenom)
			},
			supplies:      map[string]int64{chain.USDBaseDenom: 0},
			wantAvailable: true,
		},
		{
			name: "priced and settlement-priced sum into the recognised total",
			seed: func() {
				s.setAssets(chain.USDBaseDenom)
				s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
				s.plans[chain.KRWBaseDenom] = assettypes.SettlementPlan{
					Denom:            chain.KRWBaseDenom,
					RedemptionRate:   math.LegacyNewDecWithPrec(5, 1),
					OpenedHeight:     1,
					ActivationHeight: 1_000,
				}
			},
			supplies: map[string]int64{chain.KRWBaseDenom: 50, chain.USDBaseDenom: 100},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
			},
			wantPriced:     "100",
			wantSettlement: "100",
			wantAvailable:  true,
		},
		{
			name: "stale rate zeroes the priced bucket but keeps the disclosure lists",
			seed: func() {
				s.setAssets(chain.KRWBaseDenom)
				s.seedAsset(chain.SDRBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
				s.seedAsset(chain.USDBaseDenom, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF)
			},
			supplies: map[string]int64{
				chain.KRWBaseDenom: 100,
				chain.SDRBaseDenom: 40,
				chain.USDBaseDenom: 60,
			},
			expectRates: func() {
				s.setRates(oracletypes.RateSet{})
			},
			wantUntrusted:  []sdk.Coin{sdk.NewInt64Coin(chain.SDRBaseDenom, 40)},
			wantWrittenOff: []types.WrittenOffExposure{writeOff(chain.USDBaseDenom, 60)},
			wantAvailable:  false,
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
			s.bankKeeper.EXPECT().GetBalance(s.ctx, gomock.Any(), chain.NoahBaseDenom).
				Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(4)

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
			s.Require().Equal(
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, wantPriced),
				response.PricedLiabilityNoahEquivalent,
			)
			s.Require().Equal(
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, wantSettlement),
				response.SettlementLiabilityNoahEquivalent,
			)
			wantUntrusted := test.wantUntrusted
			if wantUntrusted == nil {
				wantUntrusted = []sdk.Coin{}
			}
			s.Require().Equal(wantUntrusted, response.UntrustedSuspendedSupply)
			wantWrittenOff := test.wantWrittenOff
			if wantWrittenOff == nil {
				wantWrittenOff = []types.WrittenOffExposure{}
			}
			s.Require().Equal(wantWrittenOff, response.WrittenOffExposure)
			s.Require().Equal(test.wantAvailable, response.TotalLiabilityAvailable)

			// The recognised total is priced plus settlement-priced when
			// available, and reported as zero — never a guess — otherwise.
			wantNominal := math.LegacyZeroDec()
			if test.wantAvailable {
				wantNominal = wantPriced.Add(wantSettlement)
			}
			s.Require().Equal(
				sdk.NewDecCoinFromDec(chain.NoahBaseDenom, wantNominal),
				response.NominalLiabilityNoahEquivalent,
			)
		})
	}
}
