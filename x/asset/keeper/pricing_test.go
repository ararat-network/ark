package keeper_test

import (
	"context"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	chain "ark/pkg/chain"
	"ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

// pricingDenom is registered by the pricing tests themselves so each case
// controls the status under test.
const pricingDenom = chain.USDBaseDenom

func (s *KeeperTestSuite) seedPricingAsset(status types.AssetStatus) {
	asset := types.Asset{Denom: pricingDenom, Status: status, Version: 1}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
}

func (s *KeeperTestSuite) seedPricingPlan(rate math.LegacyDec) {
	s.Require().NoError(s.keeper.SettlementPlans.Set(s.ctx, pricingDenom, types.SettlementPlan{
		Denom:                 pricingDenom,
		RedemptionRate:        rate,
		OpenedHeight:          1,
		ActivationHeight:      2,
		EarliestClosingHeight: 3,
	}))
}

// TestPricingsRequestsMembersOnly pins the gate: only oracle-priced members reach
// the Oracle, so a suspended asset's still-running feed can never shadow the
// settlement rate that prices it.
func (s *KeeperTestSuite) TestPricingsRequestsMembersOnly() {
	s.seedPricingAsset(types.AssetStatus_ASSET_STATUS_SUSPENDED)
	s.seedPricingPlan(math.LegacyNewDec(5))
	active := types.Asset{
		Denom:   chain.KRWBaseDenom,
		Status:  types.AssetStatus_ASSET_STATUS_ACTIVE,
		Version: 1,
	}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, active.Denom, active))

	s.oracleKeeper.EXPECT().
		GetAvailableRateSet(gomock.Any(), chain.KRWBaseDenom).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			rates := oracletypes.NewRateSet()
			rates[chain.KRWBaseDenom] = math.LegacyNewDec(3)
			return rates, nil
		})

	pricings, err := s.keeper.Pricings(
		s.ctx,
		pricingDenom,
		chain.KRWBaseDenom,
		chain.NoahBaseDenom,
	)

	s.Require().NoError(err)
	s.Require().Equal(types.PriceSource_PRICE_SOURCE_SETTLEMENT, pricings[pricingDenom].Source)
	s.Require().Equal(types.PriceSource_PRICE_SOURCE_ORACLE, pricings[chain.KRWBaseDenom].Source)
	s.Require().Equal(types.PriceSource_PRICE_SOURCE_NUMERAIRE, pricings[chain.NoahBaseDenom].Source)
	s.Require().True(pricings[chain.NoahBaseDenom].Rate.Equal(math.LegacyOneDec()))
}

// TestPricingsReportsUnregisteredDenoms pins that a denomination outside the
// registry is answered rather than omitted: the registry says "not mine",
// which is a verdict another authority may later answer for. The strict mock
// fails the test if such a denomination reaches the Oracle.
func (s *KeeperTestSuite) TestPricingsReportsUnregisteredDenoms() {
	pricings, err := s.keeper.Pricings(s.ctx, "uunknown")

	s.Require().NoError(err)
	s.Require().False(pricings["uunknown"].IsPriced())
	s.Require().Equal(types.UnpricedReason_UNPRICED_REASON_UNRECOGNISED, pricings["uunknown"].Reason)
}

// TestPricingsAnswersNumeraireWithoutTheOracle pins that NOAH alone never
// reaches the Oracle: the answer is the identity, and the strict mock fails
// the test if a request is made.
func (s *KeeperTestSuite) TestPricingsAnswersNumeraireWithoutTheOracle() {
	pricings, err := s.keeper.Pricings(s.ctx, chain.NoahBaseDenom)

	s.Require().NoError(err)
	s.Require().Len(pricings, 1)
	s.Require().True(pricings[chain.NoahBaseDenom].IsPriced())
}

// TestPricingsAnswersNumeraireUnasked pins that NOAH is always in the fold.
// Consumers convert through the numeraire whether or not they named it, so a
// set that omitted it could not price anything at all.
func (s *KeeperTestSuite) TestPricingsAnswersNumeraireUnasked() {
	pricings, err := s.keeper.Pricings(s.ctx)

	s.Require().NoError(err)
	s.Require().Equal(types.PriceSource_PRICE_SOURCE_NUMERAIRE, pricings[chain.NoahBaseDenom].Source)
}

// TestPricingsOmitsUnavailableFeeds pins that one member the Oracle cannot
// price costs that member its rate and nothing more: the rest of the fold still
// answers, and the caller sees which denomination could not be priced. The
// verdict carries the last rate the Oracle stored for it, which keeps the
// member countable in a total over outstanding supply while Priced stays false
// so nothing quotes or pays against it.
func (s *KeeperTestSuite) TestPricingsOmitsUnavailableFeeds() {
	s.seedPricingAsset(types.AssetStatus_ASSET_STATUS_ACTIVE)
	priced := types.Asset{
		Denom:   chain.KRWBaseDenom,
		Status:  types.AssetStatus_ASSET_STATUS_ACTIVE,
		Version: 1,
	}
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, priced.Denom, priced))

	s.oracleKeeper.EXPECT().
		GetAvailableRateSet(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			rates := oracletypes.NewRateSet()
			rates[chain.KRWBaseDenom] = math.LegacyNewDec(3)
			return rates, nil
		})
	// Exactly the unpriceable member is asked for, and only after the fresh
	// read has failed for it: the stale read is a fallback, never a first
	// resort.
	s.oracleKeeper.EXPECT().
		GetLastKnownRateSet(gomock.Any(), pricingDenom).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			rates := oracletypes.NewRateSet()
			rates[pricingDenom] = math.LegacyNewDec(9)
			return rates, nil
		})

	pricings, err := s.keeper.Pricings(s.ctx, pricingDenom, chain.KRWBaseDenom)

	s.Require().NoError(err)
	s.Require().False(pricings[pricingDenom].IsPriced())
	s.Require().Equal(types.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE, pricings[pricingDenom].Reason)
	s.Require().True(math.LegacyNewDec(9).Equal(*pricings[pricingDenom].LastRate))
	// The rate stays out of the transactable field, so a consumer reading Rate
	// still sees nothing to trade on.
	s.Require().Nil(pricings[pricingDenom].Rate)
	s.Require().True(pricings[chain.KRWBaseDenom].IsPriced())
	s.Require().Nil(pricings[chain.KRWBaseDenom].LastRate)
}

// TestPricingsLeavesNeverPricedMemberWithoutLastRate pins the one case the
// fallback cannot answer: a member the Oracle has no stored rate for at all
// keeps a nil LastRate rather than borrowing a number from somewhere else.
func (s *KeeperTestSuite) TestPricingsLeavesNeverPricedMemberWithoutLastRate() {
	s.seedPricingAsset(types.AssetStatus_ASSET_STATUS_ACTIVE)

	s.oracleKeeper.EXPECT().
		GetAvailableRateSet(gomock.Any(), pricingDenom).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			return oracletypes.NewRateSet(), nil
		})
	s.oracleKeeper.EXPECT().
		GetLastKnownRateSet(gomock.Any(), pricingDenom).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			return oracletypes.NewRateSet(), nil
		})

	pricings, err := s.keeper.Pricings(s.ctx, pricingDenom)

	s.Require().NoError(err)
	s.Require().Equal(types.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE, pricings[pricingDenom].Reason)
	s.Require().Nil(pricings[pricingDenom].LastRate)
}

// TestPricingsDeduplicatesDenoms pins that a repeated denomination is read and
// requested once: the fold is keyed by denomination, so a caller passing the
// same one twice must not pay for it twice.
func (s *KeeperTestSuite) TestPricingsDeduplicatesDenoms() {
	s.seedPricingAsset(types.AssetStatus_ASSET_STATUS_ACTIVE)

	s.oracleKeeper.EXPECT().
		GetAvailableRateSet(gomock.Any(), pricingDenom).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			rates := oracletypes.NewRateSet()
			rates[pricingDenom] = math.LegacyNewDec(2)
			return rates, nil
		})

	pricings, err := s.keeper.Pricings(s.ctx, pricingDenom, pricingDenom)

	s.Require().NoError(err)
	s.Require().True(pricings[pricingDenom].IsPriced())
}
