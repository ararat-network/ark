package keeper_test

import (
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	"github.com/ararat-network/ark/x/oracle/types"
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
		expectPer math.LegacyDec
		expectErr bool
	}{
		{
			name:      "invalid denom rejected",
			req:       &types.QueryExchangeRateRequest{Denom: "/"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name:      "noah denom returns one",
			req:       &types.QueryExchangeRateRequest{Denom: chain.NoahBaseDenom},
			expect:    math.LegacyOneDec(),
			expectPer: math.LegacyOneDec(),
		},
		{
			// Four NOAH per unit reads back as a quarter of a unit per NOAH:
			// the second reading is the reciprocal, pinned with an exact one.
			name: "stored denom returned in both readings",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
					Denom:          chain.USDBaseDenom,
					Rate:           math.LegacyNewDec(4),
					BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
				}))
			},
			req:       &types.QueryExchangeRateRequest{Denom: chain.USDBaseDenom},
			expect:    math.LegacyNewDec(4),
			expectPer: math.LegacyNewDecWithPrec(25, 2),
		},
		{
			// The reciprocal of a rate at the store bound sits below Dec
			// precision, and the query answers zero for it rather than failing.
			name: "rate at the store bound reads zero units per noah",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
					Denom:          chain.USDBaseDenom,
					Rate:           types.MaxExchangeRate,
					BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
				}))
			},
			req:       &types.QueryExchangeRateRequest{Denom: chain.USDBaseDenom},
			expect:    types.MaxExchangeRate,
			expectPer: math.LegacyZeroDec(),
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
			s.Require().True(tc.expectPer.Equal(resp.UnitsPerNoah), "expected %s per NOAH, got %s", tc.expectPer, resp.UnitsPerNoah)
		})
	}
}

// TestQueryExchangeRates pins the list shape: one entry per fresh rate, in
// denomination order, each carrying the stored figure and its reciprocal on
// the same line. A stale rate is absent, not present with a note.
func (s *KeeperTestSuite) TestQueryExchangeRates() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyNewDecWithPrec(5, 1),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.EURBaseDenom, types.ExchangeRate{
		Denom:          chain.EURBaseDenom,
		Rate:           math.LegacyNewDec(8),
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

	expected := []types.ExchangeRateReading{
		{Denom: chain.EURBaseDenom, ExchangeRate: math.LegacyNewDec(8), UnitsPerNoah: math.LegacyNewDecWithPrec(125, 3)},
		{Denom: chain.USDBaseDenom, ExchangeRate: math.LegacyNewDecWithPrec(5, 1), UnitsPerNoah: math.LegacyNewDec(2)},
	}
	s.Require().Len(resp.ExchangeRates, len(expected))
	for i, want := range expected {
		got := resp.ExchangeRates[i]
		s.Require().Equal(want.Denom, got.Denom)
		s.Require().True(want.ExchangeRate.Equal(got.ExchangeRate), "%s: expected %s, got %s", want.Denom, want.ExchangeRate, got.ExchangeRate)
		s.Require().True(want.UnitsPerNoah.Equal(got.UnitsPerNoah), "%s: expected %s per NOAH, got %s", want.Denom, want.UnitsPerNoah, got.UnitsPerNoah)
	}
}

func (s *KeeperTestSuite) TestQueryFeeds() {
	denoms := []string{chain.KRWBaseDenom, chain.USDBaseDenom}
	transitions := []types.FeedTransition{{
		Denom:                "agold",
		Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
		ActivationVoteHeight: 10,
	}}
	stored := types.Feeds{
		Denoms:      denoms,
		Version:     types.InitialFeedVersion,
		Transitions: transitions,
	}
	s.Require().NoError(s.keeper.Feeds.Set(s.ctx, stored))

	resp, err := s.queryClient.Feeds(s.ctx, &types.QueryFeedsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(stored, resp.Feeds)
}

// TestQueryFeedReferents covers the operator-facing half of the removal
// guard: the query answers from the same collector MsgRemoveFeed consults, so
// what an author inspects beforehand is what governance is judged against.
func (s *KeeperTestSuite) TestQueryFeedReferents() {
	adding := types.Feeds{
		Denoms:  []string{chain.USDBaseDenom},
		Version: types.InitialFeedVersion,
		Transitions: []types.FeedTransition{{
			Denom:                feedGold,
			Direction:            types.FeedDirection_FEED_DIRECTION_ADD,
			ActivationVoteHeight: 10,
		}},
	}
	referentAssetGold := "asset " + feedGold + " (PENDING)"

	tests := []struct {
		name      string
		setup     func()
		req       *types.QueryFeedReferentsRequest
		code      codes.Code
		expect    []types.FeedReferent
		expectErr bool
	}{
		{
			name:      "invalid denom rejected",
			req:       &types.QueryFeedReferentsRequest{Denom: "/"},
			code:      codes.InvalidArgument,
			expectErr: true,
		},
		{
			name: "denom without a feed is not found",
			setup: func() {
				s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{chain.USDBaseDenom})))
			},
			req:       &types.QueryFeedReferentsRequest{Denom: feedGold},
			code:      codes.NotFound,
			expectErr: true,
		},
		{
			name: "active feed with no claims returns empty",
			setup: func() {
				s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{chain.USDBaseDenom})))
			},
			req: &types.QueryFeedReferentsRequest{Denom: chain.USDBaseDenom},
		},
		{
			// An asset awaiting activation pins a feed that has not activated
			// yet, so Adding must be answerable rather than NotFound.
			name: "feed being added is answerable",
			setup: func() {
				s.Require().NoError(s.keeper.Feeds.Set(s.ctx, adding))
				s.keeper.SetFeedReferentGuards(stubFeedReferentGuard{
					consumer: consumerAsset,
					pinned:   map[string][]string{feedGold: {referentAssetGold}},
				})
			},
			req: &types.QueryFeedReferentsRequest{Denom: feedGold},
			expect: []types.FeedReferent{
				{Consumer: consumerAsset, Referent: referentAssetGold},
			},
		},
		{
			name: "claims are concatenated across guards",
			setup: func() {
				s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.NewFeeds([]string{chain.USDBaseDenom})))
				s.keeper.SetFeedReferentGuards(
					stubFeedReferentGuard{
						consumer: consumerAsset,
						pinned: map[string][]string{chain.USDBaseDenom: {
							referentReference,
							referentAssetUSD,
						}},
					},
					stubFeedReferentGuard{
						consumer: consumerBasket,
						pinned:   map[string][]string{chain.USDBaseDenom: {referentBasket}},
					},
				)
			},
			req: &types.QueryFeedReferentsRequest{Denom: chain.USDBaseDenom},
			expect: []types.FeedReferent{
				{Consumer: consumerAsset, Referent: referentReference},
				{Consumer: consumerAsset, Referent: referentAssetUSD},
				{Consumer: consumerBasket, Referent: referentBasket},
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.keeper.SetFeedReferentGuards()
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.queryClient.FeedReferents(s.ctx, tc.req)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				return
			}

			s.Require().NoError(err)
			s.Require().Equal(tc.expect, resp.Referents)
		})
	}
}

// TestQueryReferenceDenom routes through the query client rather than calling
// the server method directly, so the RPC name stays pinned to the proto: a
// method named anything else falls through to UnimplementedQueryServer and the
// interface assertion still compiles.
func (s *KeeperTestSuite) TestQueryReferenceDenom() {
	tests := []struct {
		name   string
		setup  func()
		expect string
	}{
		{
			// Empty is the honest answer before governance has ever configured
			// a reference, not an error.
			name: "unconfigured reference returns empty",
		},
		{
			name: "configured reference returned",
			setup: func() {
				s.Require().NoError(s.keeper.ReferenceDenom.Set(s.ctx, chain.XDRBaseDenom))
			},
			expect: chain.XDRBaseDenom,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.Require().NoError(s.keeper.ReferenceDenom.Remove(s.ctx))
			if tc.setup != nil {
				tc.setup()
			}

			resp, err := s.queryClient.ReferenceDenom(s.ctx, &types.QueryReferenceDenomRequest{})
			s.Require().NoError(err)
			s.Require().Equal(tc.expect, resp.ReferenceDenom)
		})
	}
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
