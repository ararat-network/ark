package keeper_test

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	core "noah/types"
	"noah/x/oracle/keeper"
	"noah/x/oracle/types"
)

func (s *KeeperTestSuite) queryServer() types.QueryServer {
	return keeper.NewQueryServerImpl(s.keeper)
}

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
			name:      "nil request rejected",
			req:       nil,
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "empty denom rejected",
			req:       &types.QueryExchangeRateRequest{},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "ark denom returns one",
			req:       &types.QueryExchangeRateRequest{Denom: core.MicroArkDenom},
			expect:    math.LegacyOneDec(),
			expectErr: false,
		},
		{
			name: "stored denom returned",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDec(7)))
			},
			req:       &types.QueryExchangeRateRequest{Denom: core.MicroUSDDenom},
			expect:    math.LegacyNewDec(7),
			expectErr: false,
		},
		{
			name:      "unknown denom maps to internal because keeper wraps not found",
			req:       &types.QueryExchangeRateRequest{Denom: "ufoo"},
			code:      codes.Internal,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			if tc.req == nil {
				_, err := s.queryServer().ExchangeRate(s.ctx, nil)
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			resp, err := s.queryClient.ExchangeRate(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().True(tc.expect.Equal(resp.ExchangeRate))
		})
	}
}

func (s *KeeperTestSuite) TestQueryExchangeRates() {
	tests := []struct {
		name     string
		setup    func()
		expected int
	}{
		{
			name:     "empty set",
			setup:    func() {},
			expected: 0,
		},
		{
			name: "returns all exchange rates",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDec(1000)))
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDec(1)))
			},
			expected: 2,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			resp, err := s.queryClient.ExchangeRates(s.ctx, &types.QueryExchangeRatesRequest{})
			s.Require().NoError(err)
			s.Require().Len(resp.ExchangeRates, tc.expected)
		})
	}
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
			name:      "nil request rejected",
			req:       nil,
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "empty denom rejected",
			req:       &types.QueryTobinTaxRequest{},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name: "stored tobin tax returned",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
			},
			req:       &types.QueryTobinTaxRequest{Denom: core.MicroUSDDenom},
			expect:    math.LegacyNewDecWithPrec(25, 4),
			expectErr: false,
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

			if tc.req == nil {
				_, err := s.queryServer().TobinTax(s.ctx, nil)
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
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
	tests := []struct {
		name     string
		setup    func()
		expected int
	}{
		{
			name:     "empty set",
			setup:    func() {},
			expected: 0,
		},
		{
			name: "returns all tobin taxes",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(5, 2)))
			},
			expected: 2,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			resp, err := s.queryClient.TobinTaxes(s.ctx, &types.QueryTobinTaxesRequest{})
			s.Require().NoError(err)
			s.Require().Len(resp.TobinTaxes, tc.expected)
		})
	}
}

func (s *KeeperTestSuite) TestQueryActives() {
	tests := []struct {
		name     string
		setup    func()
		expected []string
	}{
		{
			name:     "empty set",
			setup:    func() {},
			expected: nil,
		},
		{
			name: "returns all active denoms",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDec(1000)))
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDec(1)))
			},
			expected: []string{core.MicroKRWDenom, core.MicroUSDDenom},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			resp, err := s.queryClient.Actives(s.ctx, &types.QueryActivesRequest{})
			s.Require().NoError(err)
			s.Require().ElementsMatch(tc.expected, resp.Actives)
		})
	}
}

func (s *KeeperTestSuite) TestQueryVoteTargets() {
	tests := []struct {
		name     string
		setup    func()
		expected []string
	}{
		{
			name:     "empty set",
			setup:    func() {},
			expected: nil,
		},
		{
			name: "returns all vote targets",
			setup: func() {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroKRWDenom, math.LegacyNewDecWithPrec(25, 4)))
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, core.MicroUSDDenom, math.LegacyNewDecWithPrec(25, 4)))
			},
			expected: []string{core.MicroKRWDenom, core.MicroUSDDenom},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			resp, err := s.queryClient.VoteTargets(s.ctx, &types.QueryVoteTargetsRequest{})
			s.Require().NoError(err)
			s.Require().ElementsMatch(tc.expected, resp.VoteTargets)
		})
	}
}

func (s *KeeperTestSuite) TestQueryFeederDelegation() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryFeederDelegationRequest
		code      codes.Code
		expect    string
		expectErr bool
	}{
		{
			name:      "nil request rejected",
			req:       nil,
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "invalid validator address rejected",
			req:       &types.QueryFeederDelegationRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "defaults to validator account",
			req:       &types.QueryFeederDelegationRequest{ValidatorAddr: valAddr1.String()},
			expect:    sdk.AccAddress(valAddr1).String(),
			expectErr: false,
		},
		{
			name: "returns stored delegate",
			setup: func() {
				s.Require().NoError(s.keeper.FeederDelegation.Set(s.ctx, valAddr1, accAddr1))
			},
			req:       &types.QueryFeederDelegationRequest{ValidatorAddr: valAddr1.String()},
			expect:    accAddr1.String(),
			expectErr: false,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			if tc.req == nil {
				_, err := s.queryServer().FeederDelegation(s.ctx, nil)
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			resp, err := s.queryClient.FeederDelegation(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(tc.expect, resp.FeederAddr)
		})
	}
}

func (s *KeeperTestSuite) TestQueryMissCounter() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryMissCounterRequest
		code      codes.Code
		expect    uint64
		expectErr bool
	}{
		{
			name:      "nil request rejected",
			req:       nil,
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "invalid validator rejected",
			req:       &types.QueryMissCounterRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "missing counter returns zero",
			req:       &types.QueryMissCounterRequest{ValidatorAddr: valAddr1.String()},
			expect:    0,
			expectErr: false,
		},
		{
			name: "stored counter returned",
			setup: func() {
				s.Require().NoError(s.keeper.MissCounter.Set(s.ctx, valAddr1, 9))
			},
			req:       &types.QueryMissCounterRequest{ValidatorAddr: valAddr1.String()},
			expect:    9,
			expectErr: false,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			if tc.req == nil {
				_, err := s.queryServer().MissCounter(s.ctx, nil)
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			resp, err := s.queryClient.MissCounter(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(tc.expect, resp.MissCounter)
		})
	}
}

func (s *KeeperTestSuite) TestQueryAggregatePrevote() {
	tests := []struct {
		name      string
		setup     func() types.AggregateExchangeRatePrevote
		req       *types.QueryAggregatePrevoteRequest
		code      codes.Code
		expectErr bool
	}{
		{
			name:      "nil request rejected",
			setup:     func() types.AggregateExchangeRatePrevote { return types.AggregateExchangeRatePrevote{} },
			req:       nil,
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "invalid validator rejected",
			setup:     func() types.AggregateExchangeRatePrevote { return types.AggregateExchangeRatePrevote{} },
			req:       &types.QueryAggregatePrevoteRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "missing prevote returns not found",
			setup:     func() types.AggregateExchangeRatePrevote { return types.AggregateExchangeRatePrevote{} },
			req:       &types.QueryAggregatePrevoteRequest{ValidatorAddr: valAddr2.String()},
			code:      codes.NotFound,
			expectErr: true,
		},
		{
			name: "stored prevote returned",
			setup: func() types.AggregateExchangeRatePrevote {
				prevote := types.NewAggregateExchangeRatePrevote(types.GetAggregateVoteHash("salt", "1.0"+core.MicroUSDDenom, valAddr1), valAddr1, 10)
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote))
				return prevote
			},
			req: &types.QueryAggregatePrevoteRequest{ValidatorAddr: valAddr1.String()},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			expected := tc.setup()

			if tc.req == nil {
				_, err := s.queryServer().AggregatePrevote(s.ctx, nil)
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			resp, err := s.queryClient.AggregatePrevote(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(expected, resp.AggregatePrevote)
		})
	}
}

func (s *KeeperTestSuite) TestQueryAggregatePrevotes() {
	tests := []struct {
		name     string
		setup    func()
		expected int
	}{
		{
			name:     "empty set",
			setup:    func() {},
			expected: 0,
		},
		{
			name: "returns all prevotes",
			setup: func() {
				prevote1 := types.NewAggregateExchangeRatePrevote(types.GetAggregateVoteHash("salt", "1.0"+core.MicroUSDDenom, valAddr1), valAddr1, 10)
				prevote2 := types.NewAggregateExchangeRatePrevote(types.GetAggregateVoteHash("salt", "2.0"+core.MicroKRWDenom, valAddr2), valAddr2, 11)
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr1, prevote1))
				s.Require().NoError(s.keeper.AggregateExchangeRatePrevote.Set(s.ctx, valAddr2, prevote2))
			},
			expected: 2,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			resp, err := s.queryClient.AggregatePrevotes(s.ctx, &types.QueryAggregatePrevotesRequest{})
			s.Require().NoError(err)
			s.Require().Len(resp.AggregatePrevotes, tc.expected)
		})
	}
}

func (s *KeeperTestSuite) TestQueryAggregateVote() {
	tests := []struct {
		name      string
		setup     func() types.AggregateExchangeRateVote
		req       *types.QueryAggregateVoteRequest
		code      codes.Code
		expectErr bool
	}{
		{
			name:      "nil request rejected",
			setup:     func() types.AggregateExchangeRateVote { return types.AggregateExchangeRateVote{} },
			req:       nil,
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "invalid validator rejected",
			setup:     func() types.AggregateExchangeRateVote { return types.AggregateExchangeRateVote{} },
			req:       &types.QueryAggregateVoteRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "missing vote returns not found",
			setup:     func() types.AggregateExchangeRateVote { return types.AggregateExchangeRateVote{} },
			req:       &types.QueryAggregateVoteRequest{ValidatorAddr: valAddr2.String()},
			code:      codes.NotFound,
			expectErr: true,
		},
		{
			name: "stored vote returned",
			setup: func() types.AggregateExchangeRateVote {
				vote := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{
					{Denom: core.MicroUSDDenom, ExchangeRate: math.LegacyNewDec(1)},
				}, valAddr1)
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, vote))
				return vote
			},
			req: &types.QueryAggregateVoteRequest{ValidatorAddr: valAddr1.String()},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			expected := tc.setup()

			if tc.req == nil {
				_, err := s.queryServer().AggregateVote(s.ctx, nil)
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			resp, err := s.queryClient.AggregateVote(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(expected, resp.AggregateVote)
		})
	}
}

func (s *KeeperTestSuite) TestQueryAggregateVotes() {
	tests := []struct {
		name     string
		setup    func()
		expected int
	}{
		{
			name:     "empty set",
			setup:    func() {},
			expected: 0,
		},
		{
			name: "returns all votes",
			setup: func() {
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr1, types.NewAggregateExchangeRateVote(
					types.ExchangeRateTuples{{Denom: core.MicroUSDDenom, ExchangeRate: math.LegacyNewDec(1)}},
					valAddr1,
				)))
				s.Require().NoError(s.keeper.AggregateExchangeRateVote.Set(s.ctx, valAddr2, types.NewAggregateExchangeRateVote(
					types.ExchangeRateTuples{{Denom: core.MicroKRWDenom, ExchangeRate: math.LegacyNewDec(1000)}},
					valAddr2,
				)))
			},
			expected: 2,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			tc.setup()

			resp, err := s.queryClient.AggregateVotes(s.ctx, &types.QueryAggregateVotesRequest{})
			s.Require().NoError(err)
			s.Require().Len(resp.AggregateVotes, tc.expected)
		})
	}
}
