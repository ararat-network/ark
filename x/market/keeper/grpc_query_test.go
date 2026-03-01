package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	core "noah/types"
	"noah/x/market/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	res, err := s.queryClient.Params(s.goCtx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Compare with keeper state
	params, err := s.marketKeeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(params.BasePool.Equal(res.Params.BasePool))
	s.Require().Equal(params.PoolRecoveryPeriod, res.Params.PoolRecoveryPeriod)
	s.Require().True(params.MinStabilitySpread.Equal(res.Params.MinStabilitySpread))
}

func (s *KeeperTestSuite) TestQuerySwap() {
	// Set oracle rates for uusd → ukrw (noah→noah)
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "uusd").
		Return(math.LegacyOneDec(), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), "ukrw").
		Return(math.LegacyNewDec(1300), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetArkExchangeRate(gomock.Any(), core.MicroSDRDenom).
		Return(math.LegacyNewDecWithPrec(17, 1), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "uusd").
		Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes()
	s.oracleKeeper.EXPECT().GetTobinTax(gomock.Any(), "ukrw").
		Return(math.LegacyNewDecWithPrec(25, 4), nil).AnyTimes()

	res, err := s.queryClient.Swap(s.goCtx, &types.QuerySwapRequest{
		OfferCoin: "1000000uusd",
		AskDenom:  "ukrw",
	})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal("ukrw", res.ReturnCoin.Denom)
	s.Require().True(res.ReturnCoin.Amount.IsPositive())
}

func (s *KeeperTestSuite) TestQuerySwap_RecursiveSwap() {
	_, err := s.queryClient.Swap(s.goCtx, &types.QuerySwapRequest{
		OfferCoin: "1000000uusd",
		AskDenom:  "uusd",
	})
	s.Require().Error(err)
	s.Require().ErrorContains(err, "recursive swap")
}

func (s *KeeperTestSuite) TestQuerySwap_EmptyOfferCoin() {
	_, err := s.queryClient.Swap(s.goCtx, &types.QuerySwapRequest{
		OfferCoin: "",
		AskDenom:  "ukrw",
	})
	s.Require().Error(err)
	s.Require().ErrorContains(err, "invalid decimal coin expression")
}

func (s *KeeperTestSuite) TestQuerySwap_EmptyAskDenom() {
	_, err := s.queryClient.Swap(s.goCtx, &types.QuerySwapRequest{
		OfferCoin: "1000000uusd",
		AskDenom:  "",
	})
	s.Require().Error(err)
	s.Require().ErrorContains(err, "invalid ask denom")
}

func (s *KeeperTestSuite) TestQueryNoahPoolDelta() {
	// Set a known pool delta
	knownDelta := math.LegacyNewDec(98765)
	err := s.marketKeeper.NoahPoolDelta.Set(s.ctx, knownDelta)
	s.Require().NoError(err)

	res, err := s.queryClient.NoahPoolDelta(s.goCtx, &types.QueryNoahPoolDeltaRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().True(knownDelta.Equal(res.NoahPoolDelta))
}
