package keeper_test

import (
	"context"
	"errors"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	querytypes "github.com/cosmos/cosmos-sdk/types/query"

	chain "ark/pkg/chain"
	"ark/x/asset/keeper"
	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

// stubQueryRates answers the per-denom rate lookups the query server makes,
// serving the supplied rates and reporting every other denomination as one the
// Oracle does not price.
func (s *KeeperTestSuite) stubQueryRates(rates oracletypes.RateSet) {
	s.oracleKeeper.EXPECT().
		GetAvailableRateSet(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			captured := oracletypes.RateSet{
				chain.NoahBaseDenom: math.LegacyOneDec(),
			}
			// The available-set read omits what it cannot price rather than
			// failing, so an absent denomination is absent from the answer.
			for _, denom := range denoms {
				if rate, found := rates[denom]; found {
					captured[denom] = rate
				}
			}

			return captured, nil
		}).
		AnyTimes()
}

func (s *KeeperTestSuite) TestQueryAsset() {
	asset := assettypes.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	rate := math.LegacyNewDec(2)
	s.stubQueryRates(oracletypes.RateSet{asset.Denom: rate})
	server := keeper.NewQueryServerImpl(s.keeper)

	response, err := server.Asset(
		s.ctx,
		&assettypes.QueryAssetRequest{Denom: asset.Denom},
	)
	s.Require().NoError(err)
	s.Require().Equal(asset, response.PricedAsset.Asset)
	s.Require().Equal(
		assettypes.PriceSource_PRICE_SOURCE_ORACLE,
		response.PricedAsset.Source,
	)
	s.Require().Equal(
		assettypes.UnpricedReason_UNPRICED_REASON_UNSPECIFIED,
		response.PricedAsset.Reason,
	)
	s.Require().NotNil(response.PricedAsset.Rate)
	s.Require().Equal(rate, *response.PricedAsset.Rate)
	s.Require().Nil(response.PricedAsset.LastRate)

	_, err = server.Asset(
		s.ctx,
		&assettypes.QueryAssetRequest{Denom: "aunknown"},
	)
	s.requireQueryCode(err, codes.NotFound)

	_, err = server.Asset(
		s.ctx,
		&assettypes.QueryAssetRequest{Denom: "AUSD"},
	)
	s.requireQueryCode(err, codes.InvalidArgument)

	_, err = server.Asset(s.ctx, nil)
	s.requireQueryCode(err, codes.InvalidArgument)
}

// TestQueryAssets checks that the registry is returned whole and in key order,
// every member carrying the verdict the valuation fold would give it.
func (s *KeeperTestSuite) TestQueryAssets() {
	assets := assettypes.DefaultGenesisState().Assets[:3]
	rates := oracletypes.RateSet{}
	for i, asset := range assets {
		s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
		rates[asset.Denom] = math.LegacyNewDec(int64(i + 2))
	}
	s.stubQueryRates(rates)
	server := keeper.NewQueryServerImpl(s.keeper)

	response, err := server.Assets(s.ctx, &assettypes.QueryAssetsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(
		[]string{assets[0].Denom, assets[1].Denom, assets[2].Denom},
		assetDenoms(response.PricedAssets),
	)
	for _, priced := range response.PricedAssets {
		s.Require().Equal(
			assettypes.PriceSource_PRICE_SOURCE_ORACLE,
			priced.Source,
		)
		s.Require().NotNil(priced.Rate)
		s.Require().Equal(rates[priced.Asset.Denom], *priced.Rate)
	}

	_, err = server.Assets(s.ctx, nil)
	s.requireQueryCode(err, codes.InvalidArgument)
}

func (s *KeeperTestSuite) TestQuerySettlementPlan() {
	asset := assettypes.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	plan := assettypes.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        math.LegacyOneDec(),
		ActivationHeight:      10,
		EarliestClosingHeight: 110,
		OpenedHeight:          1,
	}
	s.Require().NoError(s.keeper.SettlementPlans.Set(
		s.ctx,
		asset.Denom,
		plan,
	))
	server := keeper.NewQueryServerImpl(s.keeper)

	response, err := server.SettlementPlan(
		s.ctx,
		&assettypes.QuerySettlementPlanRequest{Denom: asset.Denom},
	)
	s.Require().NoError(err)
	s.Require().Equal(plan, response.SettlementPlan)

	_, err = server.SettlementPlan(
		s.ctx,
		&assettypes.QuerySettlementPlanRequest{Denom: "aunknown"},
	)
	s.requireQueryCode(err, codes.NotFound)

	_, err = server.SettlementPlan(
		s.ctx,
		&assettypes.QuerySettlementPlanRequest{Denom: "invalid denom"},
	)
	s.requireQueryCode(err, codes.InvalidArgument)

	_, err = server.SettlementPlan(s.ctx, nil)
	s.requireQueryCode(err, codes.InvalidArgument)
}

func (s *KeeperTestSuite) TestQueryResolutionHistoryPagination() {
	assets := assettypes.DefaultGenesisState().Assets[:2]
	for _, asset := range assets {
		s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	}
	versions := []uint64{2, 4, 7}
	for _, version := range versions {
		record := assettypes.ResolutionRecord{
			Denom:   assets[0].Denom,
			Version: version,
		}
		s.Require().NoError(s.keeper.ResolutionRecords.Set(
			s.ctx,
			record.Key(),
			record,
		))
	}
	otherRecord := assettypes.ResolutionRecord{
		Denom:   assets[1].Denom,
		Version: 3,
	}
	s.Require().NoError(s.keeper.ResolutionRecords.Set(
		s.ctx,
		otherRecord.Key(),
		otherRecord,
	))
	server := keeper.NewQueryServerImpl(s.keeper)

	first, err := server.ResolutionHistory(
		s.ctx,
		&assettypes.QueryResolutionHistoryRequest{
			Denom: assets[0].Denom,
			Pagination: &querytypes.PageRequest{
				Limit:      2,
				CountTotal: true,
			},
		},
	)
	s.Require().NoError(err)
	s.Require().Equal([]uint64{2, 4}, writeOffVersions(first.ResolutionRecords))
	s.Require().Equal(uint64(3), first.Pagination.Total)
	s.Require().NotEmpty(first.Pagination.NextKey)

	second, err := server.ResolutionHistory(
		s.ctx,
		&assettypes.QueryResolutionHistoryRequest{
			Denom: assets[0].Denom,
			Pagination: &querytypes.PageRequest{
				Key:   first.Pagination.NextKey,
				Limit: 2,
			},
		},
	)
	s.Require().NoError(err)
	s.Require().Equal([]uint64{7}, writeOffVersions(second.ResolutionRecords))

	_, err = server.ResolutionHistory(
		s.ctx,
		&assettypes.QueryResolutionHistoryRequest{
			Denom: "aunknown",
		},
	)
	s.requireQueryCode(err, codes.NotFound)

	_, err = server.ResolutionHistory(
		s.ctx,
		&assettypes.QueryResolutionHistoryRequest{
			Denom: assets[0].Denom,
			Pagination: &querytypes.PageRequest{
				Reverse: true,
			},
		},
	)
	s.requireQueryCode(err, codes.InvalidArgument)

	_, err = server.ResolutionHistory(
		s.ctx,
		&assettypes.QueryResolutionHistoryRequest{
			Denom: assets[0].Denom,
			Pagination: &querytypes.PageRequest{
				Key:    first.Pagination.NextKey,
				Offset: 1,
			},
		},
	)
	s.requireQueryCode(err, codes.InvalidArgument)

	_, err = server.ResolutionHistory(
		s.ctx,
		&assettypes.QueryResolutionHistoryRequest{Denom: "AUSD"},
	)
	s.requireQueryCode(err, codes.InvalidArgument)

	_, err = server.ResolutionHistory(s.ctx, nil)
	s.requireQueryCode(err, codes.InvalidArgument)
}

// TestQueryAssetPricingByStatus pins each lifecycle status to the verdict the
// query reports for it. Exactly one of source and reason is set in every case:
// a priced status names the authority behind its rate, and an unpriced one says
// why nothing stands behind it rather than failing the query.
func (s *KeeperTestSuite) TestQueryAssetPricingByStatus() {
	tests := []struct {
		name         string
		status       assettypes.AssetStatus
		unpriced     bool
		expectSource assettypes.PriceSource
		expectReason assettypes.UnpricedReason
	}{
		{
			name:         "active with a fresh rate",
			status:       assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
			expectSource: assettypes.PriceSource_PRICE_SOURCE_ORACLE,
		},
		{
			name:         "issuance halted keeps pricing its exits",
			status:       assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED,
			expectSource: assettypes.PriceSource_PRICE_SOURCE_ORACLE,
		},
		{
			// The feed keeps running under suspension, so a rate is there to be
			// read. It loses to the status: with no settlement plan committed,
			// the market rate is exactly what stopped being trustworthy.
			name:         "suspended with a live feed and no plan",
			status:       assettypes.AssetStatus_ASSET_STATUS_SUSPENDED,
			expectReason: assettypes.UnpricedReason_UNPRICED_REASON_UNTRUSTED,
		},
		{
			name:         "written off",
			status:       assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
			expectReason: assettypes.UnpricedReason_UNPRICED_REASON_WRITTEN_OFF,
		},
		{
			name:         "retired",
			status:       assettypes.AssetStatus_ASSET_STATUS_RETIRED,
			expectReason: assettypes.UnpricedReason_UNPRICED_REASON_RETIRED,
		},
		{
			// Stale and never-priced are both omissions from the available
			// set rather than errors, so the query reports one reason without a
			// per-error-class rule of its own.
			name:         "active with a stale or missing rate",
			status:       assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
			unpriced:     true,
			expectReason: assettypes.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.SetupTest()
			asset := assettypes.DefaultGenesisState().Assets[0]
			asset.Status = test.status
			s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

			rate := math.LegacyNewDec(2)
			// A suspended asset keeps its feed running, so the mock answers
			// every lookup: absence has to come from the status gate, not from
			// the Oracle declining to price it.
			s.oracleKeeper.EXPECT().
				GetAvailableRateSet(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ ...string) (oracletypes.RateSet, error) {
					captured := oracletypes.RateSet{
						chain.NoahBaseDenom: math.LegacyOneDec(),
					}
					if !test.unpriced {
						captured[asset.Denom] = rate
					}

					return captured, nil
				}).
				AnyTimes()
			// An unpriceable member triggers the last-known lookup. Answering it
			// empty keeps every case turning on its own reason, with no
			// last_rate evidence riding along to confuse what is being pinned.
			s.oracleKeeper.EXPECT().
				GetLastKnownRateSet(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ ...string) (oracletypes.RateSet, error) {
					return oracletypes.NewRateSet(), nil
				}).
				AnyTimes()
			server := keeper.NewQueryServerImpl(s.keeper)

			response, err := server.Asset(
				s.ctx,
				&assettypes.QueryAssetRequest{Denom: asset.Denom},
			)
			s.Require().NoError(err)
			s.Require().Equal(test.expectSource, response.PricedAsset.Source)
			s.Require().Equal(test.expectReason, response.PricedAsset.Reason)
			s.Require().Nil(response.PricedAsset.LastRate)
			if test.expectSource == assettypes.PriceSource_PRICE_SOURCE_UNSPECIFIED {
				s.Require().Nil(response.PricedAsset.Rate)
			} else {
				s.Require().NotNil(response.PricedAsset.Rate)
				s.Require().Equal(rate, *response.PricedAsset.Rate)
			}

			// Both RPCs answer from the same fold, so the list must agree with
			// the single lookup rather than drifting into its own policy.
			listed, err := server.Assets(s.ctx, &assettypes.QueryAssetsRequest{})
			s.Require().NoError(err)
			s.Require().Len(listed.PricedAssets, 1)
			s.Require().Equal(response.PricedAsset, listed.PricedAssets[0])
		})
	}
}

// TestQueryAssetExchangeRateFault checks that an Oracle fault, as opposed to a
// denomination it simply does not price, fails the query rather than passing
// as an unpriced asset.
func (s *KeeperTestSuite) TestQueryAssetExchangeRateFault() {
	asset := assettypes.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.oracleKeeper.EXPECT().
		GetAvailableRateSet(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("injected Oracle fault")).
		AnyTimes()
	server := keeper.NewQueryServerImpl(s.keeper)

	_, err := server.Asset(
		s.ctx,
		&assettypes.QueryAssetRequest{Denom: asset.Denom},
	)
	s.requireQueryCode(err, codes.Internal)

	_, err = server.Assets(s.ctx, &assettypes.QueryAssetsRequest{})
	s.requireQueryCode(err, codes.Internal)
}

func assetDenoms(assets []assettypes.PricedAsset) []string {
	denoms := make([]string, len(assets))
	for i, priced := range assets {
		denoms[i] = priced.Asset.Denom
	}

	return denoms
}

func writeOffVersions(records []assettypes.ResolutionRecord) []uint64 {
	versions := make([]uint64, len(records))
	for i, record := range records {
		versions[i] = record.Version
	}

	return versions
}

func (s *KeeperTestSuite) requireQueryCode(err error, code codes.Code) {
	s.Require().Error(err)
	s.Require().Equal(code, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryEmergencyMandate() {
	server := keeper.NewQueryServerImpl(s.keeper)
	s.Require().NoError(s.keeper.EmergencyMandate.Set(
		s.ctx,
		assettypes.DefaultEmergencyMandate(),
	))

	response, err := server.EmergencyMandate(
		s.ctx,
		&assettypes.QueryEmergencyMandateRequest{},
	)
	s.Require().NoError(err)
	s.Require().True(response.Mandate.IsDisabled())
	s.Require().False(response.Active)
	s.Require().Empty(response.ConsumedDenoms)

	term := s.seedLiveMandate()
	asset := assettypes.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))
	s.Require().NoError(s.keeper.EmergencySuspendAsset(
		s.ctx,
		emergencyCommittee(),
		asset.Denom,
		term,
	))

	response, err = server.EmergencyMandate(
		s.ctx,
		&assettypes.QueryEmergencyMandateRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(emergencyCommittee(), response.Mandate.Committee)
	s.Require().True(response.Active)
	s.Require().Equal([]string{asset.Denom}, response.ConsumedDenoms)
}
