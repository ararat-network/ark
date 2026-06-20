package keeper_test

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "noah/pkg/chain"
	oraclekeeper "noah/x/oracle/keeper"
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
		expect    math.LegacyDec
		expectErr bool
	}{
		{
			name:      "invalid denom rejected",
			req:       &types.QueryExchangeRateRequest{Denom: "/"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:   "ark denom returns one",
			req:    &types.QueryExchangeRateRequest{Denom: chain.MicroArkDenom},
			expect: math.LegacyOneDec(),
		},
		{
			name: "stored denom returned",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
					Denom:       chain.MicroUSDDenom,
					Rate:        math.LegacyNewDec(7),
					BlockHeight: 10,
				}))
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(12)
			},
			req:    &types.QueryExchangeRateRequest{Denom: chain.MicroUSDDenom},
			expect: math.LegacyNewDec(7),
		},
		{
			name: "stale denom maps to failed precondition",
			setup: func() {
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.MaxExchangeRateAge = 5
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
					Denom:       chain.MicroUSDDenom,
					Rate:        math.LegacyNewDec(7),
					BlockHeight: 1,
				}))
				s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(12)
			},
			req:       &types.QueryExchangeRateRequest{Denom: chain.MicroUSDDenom},
			code:      codes.FailedPrecondition,
			expectErr: true,
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

			resp, err := oraclekeeper.NewQueryServerImpl(s.keeper).ExchangeRate(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().True(tc.expect.Equal(resp.ExchangeRate), "expected %s, got %s", tc.expect, resp.ExchangeRate)
		})
	}
}

func (s *KeeperTestSuite) TestQueryExchangeRates() {
	expected := sdk.DecCoins{sdk.NewDecCoinFromDec(chain.MicroUSDDenom, math.LegacyOneDec())}
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
		Denom:       chain.MicroUSDDenom,
		Rate:        math.LegacyOneDec(),
		BlockHeight: 10,
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroKRWDenom, types.ExchangeRate{
		Denom:       chain.MicroKRWDenom,
		Rate:        math.LegacyNewDec(1000),
		BlockHeight: 1,
	}))
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = 5
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(12)

	resp, err := oraclekeeper.NewQueryServerImpl(s.keeper).ExchangeRates(s.ctx, &types.QueryExchangeRatesRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch(expected, resp.ExchangeRates)
}

func (s *KeeperTestSuite) TestQueryTobinTax() {
	tests := []struct {
		name      string
		setup     func()
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
			name: "configured tobin tax returned",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, chain.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
			},
			req:    &types.QueryTobinTaxRequest{Denom: chain.MicroUSDDenom},
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
			if tc.setup != nil {
				tc.setup()
			}

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
	expected := types.TobinTaxes{
		{Denom: chain.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
	}
	for _, tt := range expected {
		s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, tt.Denom, tt.TobinTax))
	}

	resp, err := s.queryClient.TobinTaxes(s.ctx, &types.QueryTobinTaxesRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch(expected, resp.TobinTaxes)
}

func (s *KeeperTestSuite) TestQueryActives() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroKRWDenom, newStoredExchangeRate(chain.MicroKRWDenom, math.LegacyOneDec())))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, newStoredExchangeRate(chain.MicroUSDDenom, math.LegacyOneDec())))

	resp, err := s.queryClient.Actives(s.ctx, &types.QueryActivesRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch([]string{chain.MicroKRWDenom, chain.MicroUSDDenom}, resp.Actives)
}

func (s *KeeperTestSuite) TestQueryVoteTargets() {
	tobinTaxes := types.TobinTaxes{
		{Denom: chain.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
	}
	for _, tt := range tobinTaxes {
		s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, tt.Denom, tt.TobinTax))
	}

	resp, err := s.queryClient.VoteTargets(s.ctx, &types.QueryVoteTargetsRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch([]string{chain.MicroKRWDenom, chain.MicroUSDDenom}, resp.VoteTargets)
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
