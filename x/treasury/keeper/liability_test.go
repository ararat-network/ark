package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/keeper"
	"ark/x/treasury/types"
)

// requireLiabilitySnapshot decodes the block's cached claimable aggregate.
// Treasury keeps that figure execution-local — settlement receives only a
// payment — so tests asserting the aggregate itself read it where it lives.
// Key 0x01 holds one completeness flag byte followed by the marshalled Dec,
// mirroring the unexported encoding in liability.go.
func (s *KeeperTestSuite) requireLiabilitySnapshot(expected math.LegacyDec, complete bool) {
	s.T().Helper()
	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	snapshot, err := transientStore.Get([]byte{0x01})
	s.Require().NoError(err)
	s.Require().NotEmpty(snapshot)
	wantFlag := byte(0x00)
	if complete {
		wantFlag = byte(0x01)
	}
	s.Require().Equal(wantFlag, snapshot[0])
	var liability math.LegacyDec
	s.Require().NoError(liability.Unmarshal(snapshot[1:]))
	s.Require().Equal(expected, liability)
}

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
	_, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.requireLiabilitySnapshot(math.LegacyNewDec(200), true)

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 20),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 10),
		rates,
	))

	_, err = s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.requireLiabilitySnapshot(math.LegacyNewDec(190), true)
}

// TestLiabilityIncompleteValuationCachesClaimableAggregate pins the lazy-scan
// path under partial information for the one member no aggregate can count: one
// the Oracle has never priced. It is excluded from the claimable aggregate, the
// draw funds coverage against that aggregate rather than switching off, the
// incomplete snapshot is cached (one scan, reused), and RecordSupplyChange
// advances it while preserving the incomplete flag.
func (s *KeeperTestSuite) TestLiabilityIncompleteValuationCachesClaimableAggregate() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	// akrw has no rate this block and none on record either, so there is no
	// evidence to count it by: excluded from the claimable aggregate and
	// disclosed, while ausd keeps its measured value.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.setLastKnownRates(oracletypes.RateSet{})
	// Coverage 50/100 pays half of each quoted output.
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)).Times(2)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)),
	).Return(nil).Times(2)

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	for range 2 {
		bufferPaid, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		// Coverage 50/100 of the 10-NOAH output. Priced against the complete
		// 200 aggregate this would pay 2, so the payment pins the exclusion.
		s.Require().Equal(math.NewInt(5), bufferPaid)
	}
	s.requireLiabilitySnapshot(math.LegacyNewDec(100), false)

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
		rates,
	))

	// The supply change advanced the value without forgiving the flag.
	s.requireLiabilitySnapshot(math.LegacyNewDec(90), false)
}

// TestLiabilityFeedOutageLeavesCoverageUnchanged pins the property the last
// known rate exists for: a member losing its feed must not change what anyone
// else's redemption is worth. akrw is priced at one and then loses its feed
// with the same rate on record, so the aggregate stays 200 and an ausd
// redemption pays exactly what it paid before the outage. Dropping akrw would
// halve the denominator and double this payment, handing akrw's share of the
// Buffer to whoever transacts during the outage.
func (s *KeeperTestSuite) TestLiabilityFeedOutageLeavesCoverageUnchanged() {
	drawOnce := func() math.Int {
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
			Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
		s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
			Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
		s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
			Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)).Times(1)
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
			gomock.Any(),
		).Return(nil).AnyTimes()

		bufferPaid, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 20),
			math.NewInt(20),
			oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
				chain.USDBaseDenom:  math.LegacyOneDec(),
			},
		)
		s.Require().NoError(err)
		return bufferPaid
	}

	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyOneDec(),
		chain.KRWBaseDenom: math.LegacyOneDec(),
	})
	healthy := drawOnce()
	s.requireLiabilitySnapshot(math.LegacyNewDec(200), true)
	// Coverage 50/200 of the 20-NOAH output.
	s.Require().Equal(math.NewInt(5), healthy)

	// The feed drops. Nothing about akrw's obligation changed, and the rate
	// that priced it a block ago is still on record.
	s.clearTransientStore()
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.setLastKnownRates(oracletypes.RateSet{chain.KRWBaseDenom: math.LegacyOneDec()})

	during := drawOnce()
	s.Require().Equal(healthy, during, "a feed outage elsewhere must not change this redemption")
	// The aggregate is whole, but no fresh rate stood behind part of it, so the
	// flag is false and fund targets stay unsized.
	s.requireLiabilitySnapshot(math.LegacyNewDec(200), false)
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
		_, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		s.requireLiabilitySnapshot(math.LegacyNewDec(100), true)

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
	_, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.requireLiabilitySnapshot(math.LegacyNewDec(200), true)
	// A complete valuation is the ordinary case and discloses nothing. Emitting
	// every block would bury the blocks that matter under the blocks that do
	// not.
	s.requireNoTypedEvent(&types.EventLiabilityIncomplete{})
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
	_, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyMustNewDecFromStr("0.5"),
		},
	)
	s.Require().NoError(err)
	s.requireLiabilitySnapshot(math.LegacyNewDec(200), true)
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

	_, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.requireLiabilitySnapshot(math.LegacyNewDec(100), true)
}

// TestPrimeLiabilitySnapshotFailsOnUnrepresentableConversion pins arithmetic
// out of range as a hard failure rather than an incomplete valuation. A member
// the partition prices, whose supply at its rate leaves Dec range, is state
// past the supported domain: unlike a missing rate it does not return on the
// next block, so reporting it as unavailable would retire the Buffer
// permanently behind a flag that fires for benign reasons. Incompleteness is
// reserved for exposure no honest rate could value.
func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotFailsOnUnrepresentableConversion() {
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewCoin(chain.USDBaseDenom, math.NewIntWithDecimal(1, 60))).Times(1)
	// At 1e-18 ausd per NOAH, 1e60 base units value to 1e78 NOAH — beyond
	// LegacyDec range.
	s.setRates(oracletypes.RateSet{
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(1, 18),
	})

	err := s.keeper.PrimeLiabilitySnapshot(s.ctx)
	s.Require().ErrorContains(err, "valuing liability ausd")
	s.Require().ErrorIs(err, oracletypes.ErrConversionOutOfRange)
}

// TestPrimeLiabilitySnapshotExcludesUnpricedMemberFromClaimable pins the
// preblock prime under partial information: the stale member is excluded from
// the claimable aggregate, and the block's draws fund healthy exits at
// coverage of that aggregate — with a smaller denominator than the complete
// one, so a lapse elsewhere raises per-exit funding instead of switching the
// Buffer off. The snapshot is primed once and reused without rescanning.
func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotExcludesUnpricedMemberFromClaimable() {
	s.setAssets(chain.USDBaseDenom, chain.KRWBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// Coverage 50/100 against the claimable aggregate; with akrw priced the
	// denominator would have been 200 and the payment 2.
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 50)).Times(1)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(), types.RedemptionBufferName, markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 5)),
	).Return(nil)

	// The rest of the block reuses the snapshot without rescanning.
	bufferPaid, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	// Coverage 50/100 of the 10-NOAH output; against the complete 200
	// aggregate it would have paid 2.
	s.Require().Equal(math.NewInt(5), bufferPaid)
	s.requireLiabilitySnapshot(math.LegacyNewDec(100), false)
	// The prime disclosed the degraded valuation, naming akrw and the 100 the
	// draw above divided by. The draw could not have disclosed it a second
	// time: the Times(1) supply expectations above already pin it to reusing
	// the snapshot rather than rebuilding one.
	s.requireTypedEvent(&types.EventLiabilityIncomplete{
		ClaimableLiability: chain.NoahDecCoin(math.LegacyNewDec(100)),
		StaleMemberSupply:  []sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 100)},
	})
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
	// same denom must retract it rather than leave the stale value winning:
	// the sole member is now excluded, so the claimable aggregate is zero and
	// a draw claiming against it contradicts the snapshot — an unreachable
	// state through Market, whose quote needs the same fresh rate, so the
	// contradiction fails loudly instead of being papered over.
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))
	s.setRates(oracletypes.RateSet{})
	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	_, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().ErrorContains(err, "exceeds claimable liability")
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
// is invisible; and a member the Oracle cannot price is disclosed in its own
// list while every healthy member stays priced, because a bucket may be
// partial exactly when its shortfall is enumerated beside it.
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
		wantUntrusted   []sdk.Coin
		wantWrittenOff  []types.WrittenOffExposure
		wantStaleSupply []sdk.Coin
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
					RedemptionRate:        math.LegacyNewDecWithPrec(5, 1),
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
			wantUntrusted:   []sdk.Coin{sdk.NewInt64Coin(chain.SDRBaseDenom, 40)},
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
					chain.KRWBaseDenom: math.LegacyNewDecWithPrec(5, 1),
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
			wantStaleSupply := test.wantStaleSupply
			if wantStaleSupply == nil {
				wantStaleSupply = []sdk.Coin{}
			}
			s.Require().Equal(wantStaleSupply, response.StaleMemberSupply)

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
