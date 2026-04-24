package keeper_test

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

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
			s.Require().True(tc.expect.Equal(resp.ExchangeRate))
		})
	}
}

func (s *KeeperTestSuite) TestQueryExchangeRates() {
	tests := []struct {
		name     string
		expected sdk.DecCoins
	}{
		{
			name: "empty set",
		},
		{
			name: "returns all exchange rates",
			expected: sdk.DecCoins{
				sdk.NewDecCoinFromDec(core.MicroKRWDenom, math.LegacyNewDec(1000)),
				sdk.NewDecCoinFromDec(core.MicroUSDDenom, math.LegacyNewDec(1)),
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, exchangeRate := range tc.expected {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, exchangeRate.Denom, exchangeRate.Amount))
			}

			resp, err := s.queryClient.ExchangeRates(s.ctx, &types.QueryExchangeRatesRequest{})
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, resp.ExchangeRates)
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
			name:      "invalid denom rejected",
			req:       &types.QueryTobinTaxRequest{Denom: "/"},
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
		expected types.TobinTaxes
	}{
		{
			name: "empty set",
		},
		{
			name: "returns all tobin taxes",
			expected: types.TobinTaxes{
				{Denom: core.MicroKRWDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				{Denom: core.MicroUSDDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, tobinTax := range tc.expected {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, tobinTax.Denom, tobinTax.TobinTax))
			}

			resp, err := s.queryClient.TobinTaxes(s.ctx, &types.QueryTobinTaxesRequest{})
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, resp.TobinTaxes)
		})
	}
}

func (s *KeeperTestSuite) TestQueryActives() {
	tests := []struct {
		name     string
		expected []string
	}{
		{
			name:     "empty set",
			expected: nil,
		},
		{
			name:     "returns all active exchange rate denoms",
			expected: []string{core.MicroKRWDenom, core.MicroUSDDenom},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, denom := range tc.expected {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, denom, math.LegacyOneDec()))
			}

			resp, err := s.queryClient.Actives(s.ctx, &types.QueryActivesRequest{})
			s.Require().NoError(err)
			s.Require().ElementsMatch(tc.expected, resp.Actives)
		})
	}
}

func (s *KeeperTestSuite) TestQueryVoteTargets() {
	tests := []struct {
		name     string
		expected []string
	}{
		{
			name:     "empty set",
			expected: nil,
		},
		{
			name:     "returns all vote targets",
			expected: []string{core.MicroKRWDenom, core.MicroUSDDenom},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, denom := range tc.expected {
				s.Require().NoError(s.keeper.TobinTax.Set(s.ctx, denom, math.LegacyNewDecWithPrec(25, 4)))
			}

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
			name:      "missing counter returns zero",
			req:       &types.QueryMissCountRequest{ValidatorAddr: valAddr1.String()},
			expect:    0,
			expectErr: false,
		},
		{
			name: "stored counter returned",
			setup: func() {
				s.Require().NoError(s.keeper.MissCount.Set(s.ctx, valAddr1, 9))
			},
			req:       &types.QueryMissCountRequest{ValidatorAddr: valAddr1.String()},
			expect:    9,
			expectErr: false,
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

func (s *KeeperTestSuite) TestQueryPrevote() {
	tests := []struct {
		name      string
		setup     func() types.Prevote
		req       *types.QueryPrevoteRequest
		code      codes.Code
		expectErr bool
	}{
		{
			name:      "invalid validator rejected",
			setup:     func() types.Prevote { return types.Prevote{} },
			req:       &types.QueryPrevoteRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "missing prevote returns not found",
			setup:     func() types.Prevote { return types.Prevote{} },
			req:       &types.QueryPrevoteRequest{ValidatorAddr: valAddr2.String()},
			code:      codes.NotFound,
			expectErr: true,
		},
		{
			name: "stored prevote returned",
			setup: func() types.Prevote {
				prevote := types.NewPrevote(types.GetVoteHash("salt", "1.0"+core.MicroUSDDenom, valAddr1), valAddr1, 10)
				s.Require().NoError(s.keeper.Prevote.Set(s.ctx, valAddr1, prevote))
				return prevote
			},
			req: &types.QueryPrevoteRequest{ValidatorAddr: valAddr1.String()},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			expected := tc.setup()

			resp, err := s.queryClient.Prevote(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(expected, resp.Prevote)
		})
	}
}

func (s *KeeperTestSuite) TestQueryPrevotes() {
	prevote1 := types.NewPrevote(types.GetVoteHash("salt", "1.0"+core.MicroUSDDenom, valAddr1), valAddr1, 10)
	prevote2 := types.NewPrevote(types.GetVoteHash("salt", "2.0"+core.MicroKRWDenom, valAddr2), valAddr2, 11)

	tests := []struct {
		name     string
		expected []types.Prevote
	}{
		{
			name: "empty set",
		},
		{
			name:     "returns all prevotes",
			expected: []types.Prevote{prevote1, prevote2},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, prevote := range tc.expected {
				voter, err := sdk.ValAddressFromBech32(prevote.Voter)
				s.Require().NoError(err)
				s.Require().NoError(s.keeper.Prevote.Set(s.ctx, voter, prevote))
			}

			resp, err := s.queryClient.Prevotes(s.ctx, &types.QueryPrevotesRequest{})
			s.Require().NoError(err)
			s.Require().ElementsMatch(tc.expected, resp.Prevotes)
		})
	}
}

func (s *KeeperTestSuite) TestQueryVote() {
	tests := []struct {
		name      string
		setup     func() types.Vote
		req       *types.QueryVoteRequest
		code      codes.Code
		expectErr bool
	}{
		{
			name:      "invalid validator rejected",
			setup:     func() types.Vote { return types.Vote{} },
			req:       &types.QueryVoteRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "missing vote returns not found",
			setup:     func() types.Vote { return types.Vote{} },
			req:       &types.QueryVoteRequest{ValidatorAddr: valAddr2.String()},
			code:      codes.NotFound,
			expectErr: true,
		},
		{
			name: "stored vote returned",
			setup: func() types.Vote {
				vote := types.NewVote(types.ExchangeRates{
					{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDec(1)},
				}, valAddr1)
				s.Require().NoError(s.keeper.Vote.Set(s.ctx, valAddr1, vote))
				return vote
			},
			req: &types.QueryVoteRequest{ValidatorAddr: valAddr1.String()},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			expected := tc.setup()

			resp, err := s.queryClient.Vote(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(expected, resp.Vote)
		})
	}
}

func (s *KeeperTestSuite) TestQueryVotes() {
	vote1 := types.NewVote(
		types.ExchangeRates{{Denom: core.MicroUSDDenom, Rate: math.LegacyNewDec(1)}},
		valAddr1,
	)
	vote2 := types.NewVote(
		types.ExchangeRates{{Denom: core.MicroKRWDenom, Rate: math.LegacyNewDec(1000)}},
		valAddr2,
	)

	tests := []struct {
		name     string
		expected []types.Vote
	}{
		{
			name: "empty set",
		},
		{
			name:     "returns all votes",
			expected: []types.Vote{vote1, vote2},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			for _, vote := range tc.expected {
				voter, err := sdk.ValAddressFromBech32(vote.Voter)
				s.Require().NoError(err)
				s.Require().NoError(s.keeper.Vote.Set(s.ctx, voter, vote))
			}

			resp, err := s.queryClient.Votes(s.ctx, &types.QueryVotesRequest{})
			s.Require().NoError(err)
			s.Require().ElementsMatch(tc.expected, resp.Votes)
		})
	}
}
