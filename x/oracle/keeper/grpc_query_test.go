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
			req:    &types.QueryExchangeRateRequest{Denom: chain.NoahBaseDenom},
			expect: math.LegacyOneDec(),
		},
		{
			name: "stored denom returned",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
					Denom:          chain.USDBaseDenom,
					Rate:           math.LegacyNewDec(7),
					BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
				}))
			},
			req:    &types.QueryExchangeRateRequest{Denom: chain.USDBaseDenom},
			expect: math.LegacyNewDec(7),
		},
		{
			name: "stale denom maps to failed precondition",
			setup: func() {
				params, err := s.keeper.Params.Get(s.ctx)
				s.Require().NoError(err)
				params.MaxExchangeRateAge = time.Minute
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
					Denom:          chain.USDBaseDenom,
					Rate:           math.LegacyNewDec(7),
					BlockTimestamp: oracleTestBlockTime.Add(-time.Minute - time.Second),
				}))
			},
			req:       &types.QueryExchangeRateRequest{Denom: chain.USDBaseDenom},
			code:      codes.FailedPrecondition,
			expectErr: true,
		},
		{
			name:      "unknown denom maps to not found error",
			req:       &types.QueryExchangeRateRequest{Denom: "afoo"},
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
	expected := sdk.DecCoins{sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyOneDec())}
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.KRWBaseDenom, types.ExchangeRate{
		Denom:          chain.KRWBaseDenom,
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
				params.TobinTaxes = []types.TobinTax{
					{Denom: chain.USDBaseDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
				}
				s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
			},
			req:    &types.QueryTobinTaxRequest{Denom: chain.USDBaseDenom},
			expect: math.LegacyNewDecWithPrec(25, 4),
		},
		{
			name:      "missing denom returns not found",
			req:       &types.QueryTobinTaxRequest{Denom: "afoo"},
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
	expected := []types.TobinTax{
		{Denom: chain.KRWBaseDenom, TobinTax: math.LegacyNewDecWithPrec(25, 4)},
		{Denom: chain.USDBaseDenom, TobinTax: math.LegacyNewDecWithPrec(5, 2)},
	}
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.TobinTaxes = expected
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	resp, err := s.queryClient.TobinTaxes(s.ctx, &types.QueryTobinTaxesRequest{})
	s.Require().NoError(err)
	s.Require().ElementsMatch(expected, resp.TobinTaxes)
}

func (s *KeeperTestSuite) TestQueryVoteTargets() {
	voteTargets := []string{chain.KRWBaseDenom, chain.USDBaseDenom}
	pending := &types.PendingVoteTargets{
		Denoms:               []string{chain.USDBaseDenom},
		Version:              types.InitialVoteTargetVersion + 1,
		ActivationVoteHeight: 10,
	}
	s.Require().NoError(s.keeper.VoteTargets.Set(s.ctx, types.VoteTargets{
		Denoms:  voteTargets,
		Version: types.InitialVoteTargetVersion,
		Pending: pending,
	}))

	resp, err := s.queryClient.VoteTargets(s.ctx, &types.QueryVoteTargetsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(voteTargets, resp.VoteTargets)
	s.Require().Equal(types.InitialVoteTargetVersion, resp.TargetVersion)
	s.Require().Equal(pending, resp.Pending)
}

func (s *KeeperTestSuite) TestQueryRewardWeight() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryRewardWeightRequest
		code      codes.Code
		expect    math.Int
		expectErr bool
	}{
		{
			name:      "invalid validator rejected",
			req:       &types.QueryRewardWeightRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name: "stored reward weight returned",
			setup: func() {
				s.Require().NoError(s.keeper.RewardWeight.Set(s.ctx, valAddr1, math.NewInt(9)))
			},
			req:    &types.QueryRewardWeightRequest{ValidatorAddr: valAddr1.String()},
			expect: math.NewInt(9),
		},
		{
			name:   "missing reward weight returns zero",
			req:    &types.QueryRewardWeightRequest{ValidatorAddr: valAddr2.String()},
			expect: math.ZeroInt(),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.queryClient.RewardWeight(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().True(tc.expect.Equal(resp.RewardWeight))
		})
	}
}

func (s *KeeperTestSuite) TestQueryAttendance() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryAttendanceRequest
		code      codes.Code
		expect    types.Attendance
		expectErr bool
	}{
		{
			name:      "invalid validator rejected",
			req:       &types.QueryAttendanceRequest{ValidatorAddr: "invalid"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name: "stored attendance returned",
			setup: func() {
				s.Require().NoError(s.keeper.Attendance.Set(s.ctx, valAddr1, types.Attendance{EligibleBlocks: 9, AttendedBlocks: 7}))
			},
			req:    &types.QueryAttendanceRequest{ValidatorAddr: valAddr1.String()},
			expect: types.Attendance{EligibleBlocks: 9, AttendedBlocks: 7},
		},
		{
			name:   "missing attendance returns zero value",
			req:    &types.QueryAttendanceRequest{ValidatorAddr: valAddr2.String()},
			expect: types.Attendance{},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.queryClient.Attendance(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(tc.expect, resp.Attendance)
		})
	}
}
