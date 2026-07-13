package keeper_test

import (
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oraclekeeper "ark/x/oracle/keeper"
	"ark/x/oracle/types"
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
			name:   "noah denom returns one",
			req:    &types.QueryExchangeRateRequest{Denom: chain.MicroNoahDenom},
			expect: math.LegacyOneDec(),
		},
		{
			name: "stored denom returned",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
					Denom:          chain.MicroUSDDenom,
					Rate:           math.LegacyNewDec(7),
					BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
				}))
			},
			req:    &types.QueryExchangeRateRequest{Denom: chain.MicroUSDDenom},
			expect: math.LegacyNewDec(7),
		},
		{
			name: "stale denom maps to failed precondition",
			setup: func() {
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.MaxExchangeRateAge = time.Minute
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
					Denom:          chain.MicroUSDDenom,
					Rate:           math.LegacyNewDec(7),
					BlockTimestamp: oracleTestBlockTime.Add(-time.Minute - time.Second),
				}))
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
		Denom:          chain.MicroUSDDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroKRWDenom, types.ExchangeRate{
		Denom:          chain.MicroKRWDenom,
		Rate:           math.LegacyNewDec(1000),
		BlockTimestamp: oracleTestBlockTime.Add(-2 * time.Minute),
	}))
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	resp, err := oraclekeeper.NewQueryServerImpl(s.keeper).ExchangeRates(s.ctx, &types.QueryExchangeRatesRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch(expected, resp.ExchangeRates)
}

func (s *KeeperTestSuite) TestQueryExchangeRatesRejectsInvalidStoredDenom() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.MicroUSDDenom, types.ExchangeRate{
		Denom:          "uUSD",
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime,
	}))

	_, err := oraclekeeper.NewQueryServerImpl(s.keeper).ExchangeRates(s.ctx, &types.QueryExchangeRatesRequest{})
	s.Require().Error(err)
	s.Require().Equal(codes.Internal, status.Code(err))
	s.Require().ErrorContains(err, "stored denom")
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
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.TobinTaxes = types.TobinTaxes{
					{Denom: chain.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				}
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
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
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = expected
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

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
	voteTargets := []string{chain.MicroKRWDenom, chain.MicroUSDDenom}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargetState{Denoms: voteTargets}))

	resp, err := s.queryClient.VoteTargets(s.ctx, &types.QueryVoteTargetsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(voteTargets, resp.VoteTargets)
}

func (s *KeeperTestSuite) TestQueryScoreWeight() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryScoreWeightRequest
		code      codes.Code
		expect    math.Int
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
				s.Require().NoError(s.keeper.ScoreWeight.Set(s.ctx, valAddr1, math.NewInt(9)))
			},
			req:    &types.QueryScoreWeightRequest{ValidatorAddr: valAddr1.String()},
			expect: math.NewInt(9),
		},
		{
			name:   "missing score returns zero",
			req:    &types.QueryScoreWeightRequest{ValidatorAddr: valAddr2.String()},
			expect: math.ZeroInt(),
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
			s.Require().True(tc.expect.Equal(resp.ScoreWeight))
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
