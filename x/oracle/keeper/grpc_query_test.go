package keeper_test

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	core "noah/types"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	resp, err := s.queryClient.Params(s.ctx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultParams(), resp.Params)
}

func (s *KeeperTestSuite) TestQueryExchangeRate() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryExchangeRateRequest
		code      codes.Code
		expect    types.ExchangeRate
		expectErr bool
	}{
		{
			name:      "invalid denom rejected",
			req:       &types.QueryExchangeRateRequest{Denom: "/"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name: "ark denom returns one",
			req:  &types.QueryExchangeRateRequest{Denom: core.MicroArkDenom},
			expect: types.ExchangeRate{
				Denom: core.MicroArkDenom,
				Rate:  math.LegacyOneDec(),
			},
		},
		{
			name: "stored denom returned",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, newStoredExchangeRate(core.MicroUSDDenom, math.LegacyNewDec(7))))
			},
			req: &types.QueryExchangeRateRequest{Denom: core.MicroUSDDenom},
			expect: types.ExchangeRate{
				Denom: core.MicroUSDDenom,
				Rate:  math.LegacyNewDec(7),
			},
		},
		{
			name:      "unknown denom maps to not found error",
			req:       &types.QueryExchangeRateRequest{Denom: "ufoo"},
			code:      codes.NotFound,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.queryClient.ExchangeRate(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(tc.expect.Denom, resp.ExchangeRate.Denom)
			s.Require().True(tc.expect.Rate.Equal(resp.ExchangeRate.Rate))
		})
	}
}

func (s *KeeperTestSuite) TestQueryExchangeRates() {
	expected := types.ExchangeRates{
		newStoredExchangeRate(core.MicroKRWDenom, math.LegacyNewDec(1000)),
		newStoredExchangeRate(core.MicroUSDDenom, math.LegacyOneDec()),
	}
	for _, exchangeRate := range expected {
		s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, exchangeRate.Denom, exchangeRate))
	}

	resp, err := s.queryClient.ExchangeRates(s.ctx, &types.QueryExchangeRatesRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch(expected, resp.ExchangeRates)
}

func (s *KeeperTestSuite) TestQueryTobinTax() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = types.TobinTaxes{
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	tests := []struct {
		name      string
		req       *types.QueryTobinTaxRequest
		code      codes.Code
		expect    math.LegacyDec
		expectErr bool
	}{
		{
			name:      "invalid denom rejected",
			req:       &types.QueryTobinTaxRequest{Denom: "/"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:   "configured tobin tax returned",
			req:    &types.QueryTobinTaxRequest{Denom: core.MicroUSDDenom},
			expect: math.LegacyNewDecWithPrec(25, 4),
		},
		{
			name:      "missing denom returns not found",
			req:       &types.QueryTobinTaxRequest{Denom: "ufoo"},
			code:      codes.NotFound,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			resp, err := s.queryClient.TobinTax(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().True(tc.expect.Equal(resp.TobinTax))
		})
	}
}

func (s *KeeperTestSuite) TestQueryTobinTaxes() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = types.TobinTaxes{
		{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	resp, err := s.queryClient.TobinTaxes(s.ctx, &types.QueryTobinTaxesRequest{})
	s.Require().NoError(err)
	s.Require().Equal(params.TobinTaxes, resp.TobinTaxes)
}

func (s *KeeperTestSuite) TestQueryActives() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, newStoredExchangeRate(core.MicroKRWDenom, math.LegacyOneDec())))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, newStoredExchangeRate(core.MicroUSDDenom, math.LegacyOneDec())))

	resp, err := s.queryClient.Actives(s.ctx, &types.QueryActivesRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch([]string{core.MicroKRWDenom, core.MicroUSDDenom}, resp.Actives)
}

func (s *KeeperTestSuite) TestQueryVoteTargets() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = types.TobinTaxes{
		{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
	}
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	resp, err := s.queryClient.VoteTargets(s.ctx, &types.QueryVoteTargetsRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch([]string{core.MicroKRWDenom, core.MicroUSDDenom}, resp.VoteTargets)
}

func (s *KeeperTestSuite) TestQueryScoreWeight() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryScoreWeightRequest
		code      codes.Code
		expect    uint64
		expectErr bool
	}{
		{
			name:      "invalid validator rejected",
			req:       &types.QueryScoreWeightRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name: "stored score returned",
			setup: func() {
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, 9))
			},
			req:    &types.QueryScoreWeightRequest{ValidatorAddr: valAddr1.String()},
			expect: 9,
		},
		{
			name:   "missing score returns zero",
			req:    &types.QueryScoreWeightRequest{ValidatorAddr: valAddr2.String()},
			expect: 0,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.queryClient.ScoreWeight(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(tc.expect, resp.ScoreWeight)
		})
	}
}

func (s *KeeperTestSuite) TestQueryMissCount() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryMissCountRequest
		code      codes.Code
		expect    uint64
		expectErr bool
	}{
		{
			name:      "invalid validator rejected",
			req:       &types.QueryMissCountRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name: "stored counter returned",
			setup: func() {
				s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, 9))
			},
			req:    &types.QueryMissCountRequest{ValidatorAddr: valAddr1.String()},
			expect: 9,
		},
		{
			name:   "missing counter returns zero",
			req:    &types.QueryMissCountRequest{ValidatorAddr: valAddr2.String()},
			expect: 0,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.queryClient.MissCount(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(tc.expect, resp.MissCount)
		})
	}
}
