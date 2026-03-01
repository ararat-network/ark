package keeper_test

import (
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/treasury/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	res, err := s.queryClient.Params(s.goCtx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	params, err := s.treasuryKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(params, res.Params)
}

func (s *KeeperTestSuite) TestQueryTaxRate() {
	res, err := s.queryClient.TaxRate(s.goCtx, &types.QueryTaxRateRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	taxRate, err := s.treasuryKeeper.TaxRate.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(taxRate.Equal(res.TaxRate))
}

func (s *KeeperTestSuite) TestQueryTaxCap() {
	// Set a known tax cap
	s.Require().NoError(s.treasuryKeeper.TaxCaps.Set(s.ctx, "uusd", math.NewInt(1000000)))

	res, err := s.queryClient.TaxCap(s.goCtx, &types.QueryTaxCapRequest{Denom: "uusd"})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(math.NewInt(1000000), res.TaxCap)
}

func (s *KeeperTestSuite) TestQueryTaxCap_NotFound() {
	_, err := s.queryClient.TaxCap(s.goCtx, &types.QueryTaxCapRequest{Denom: "uusd"})
	s.Require().Error(err)
	s.Require().ErrorContains(err, "tax cap not found")
}

func (s *KeeperTestSuite) TestQueryTaxCap_InvalidDenom() {
	_, err := s.queryClient.TaxCap(s.goCtx, &types.QueryTaxCapRequest{Denom: ""})
	s.Require().Error(err)
	s.Require().ErrorContains(err, "invalid denom")
}

func (s *KeeperTestSuite) TestQueryTaxCaps() {
	s.Require().NoError(s.treasuryKeeper.TaxCaps.Set(s.ctx, "uusd", math.NewInt(1000000)))
	s.Require().NoError(s.treasuryKeeper.TaxCaps.Set(s.ctx, "ukrw", math.NewInt(1300000000)))

	res, err := s.queryClient.TaxCaps(s.goCtx, &types.QueryTaxCapsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Len(res.TaxCaps, 2)
}

func (s *KeeperTestSuite) TestQueryRewardWeight() {
	res, err := s.queryClient.RewardWeight(s.goCtx, &types.QueryRewardWeightRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	rewardWeight, err := s.treasuryKeeper.RewardWeight.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(rewardWeight.Equal(res.RewardWeight))
}

func (s *KeeperTestSuite) TestQueryTaxProceeds() {
	// Record some tax proceeds
	s.Require().NoError(s.treasuryKeeper.EpochTaxProceeds.Set(s.ctx, types.EpochTaxProceeds{
		TaxProceeds: sdk.NewCoins(sdk.NewCoin("uusd", math.NewInt(5000))),
	}))

	res, err := s.queryClient.TaxProceeds(s.goCtx, &types.QueryTaxProceedsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(math.NewInt(5000), res.TaxProceeds.AmountOf("uusd"))
}

func (s *KeeperTestSuite) TestQuerySeigniorageProceeds() {
	// Set initial issuance to 1000, current supply to 800 → seigniorage = 200
	s.Require().NoError(s.treasuryKeeper.EpochInitialIssuance.Set(s.ctx, types.EpochInitialIssuance{
		Issuance: sdk.NewCoins(sdk.NewCoin(core.MicroArkDenom, math.NewInt(1000))),
	}))
	s.bankKeeper.EXPECT().GetSupply(s.ctx, core.MicroArkDenom).
		Return(sdk.NewCoin(core.MicroArkDenom, math.NewInt(800)))

	res, err := s.queryClient.SeigniorageProceeds(s.goCtx, &types.QuerySeigniorageProceedsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(math.NewInt(200), res.SeigniorageProceeds)
}
