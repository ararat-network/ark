package keeper_test

import (
	"math/big"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/keeper"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	res, err := s.queryClient.Params(s.ctx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)

	// Compare with keeper state
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(params, res.Params)
}

func (s *KeeperTestSuite) TestQuerySwap() {
	tests := []struct {
		name      string
		setup     func()
		req       *types.QuerySwapRequest
		code      codes.Code
		expectErr string
	}{
		{
			name: "empty offer coin",
			req: &types.QuerySwapRequest{
				OfferCoin: "",
				AskDenom:  "akrw",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid decimal coin expression",
		},
		{
			name: "empty ask denom",
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000ausd",
				AskDenom:  "",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid ask denom",
		},
		{
			name: "invalid offer coin format",
			req: &types.QuerySwapRequest{
				OfferCoin: "notacoin",
				AskDenom:  "akrw",
			},
			code:      codes.InvalidArgument,
			expectErr: "invalid decimal coin expression",
		},
		{
			name: "missing oracle price returns failed precondition",
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "unknown").
					Return(nil, oracletypes.ErrUnknownDenom)
			},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000ausd",
				AskDenom:  "unknown",
			},
			code:      codes.FailedPrecondition,
			expectErr: "no price registered with oracle",
		},
		{
			name: "stale oracle price returns failed precondition",
			setup: func() {
				s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "akrw").
					Return(nil, oracletypes.ErrStaleExchangeRate)
			},
			req: &types.QuerySwapRequest{
				OfferCoin: "1000000ausd",
				AskDenom:  "akrw",
			},
			code:      codes.FailedPrecondition,
			expectErr: "stale exchange rate",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			_, err := s.queryClient.Swap(s.ctx, tc.req)
			s.Require().Error(err)
			s.Require().Equal(tc.code, status.Code(err))
			s.Require().ErrorContains(err, tc.expectErr)
		})
	}
}

func (s *KeeperTestSuite) TestQuerySwapAcceptsLargeRepresentableAmount() {
	largeAmount := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 200))
	offerCoin := sdk.NewCoin("ausd", largeAmount)

	s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSet{
			"ausd":             math.LegacyOneDec(),
			chain.XDRBaseDenom: math.LegacyOneDec(),
			"akrw":             math.LegacyOneDec(),
		}, nil)
	// The subject is representability, so both legs are overridden to zero and
	// the output equals the offer exactly.
	s.seedTobinTaxOverride("ausd", math.LegacyZeroDec())
	s.seedTobinTaxOverride("akrw", math.LegacyZeroDec())

	res, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  "akrw",
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoin("akrw", largeAmount), res.SwapCoin)
}

func (s *KeeperTestSuite) TestQuerySwapDirectStableConversionAvoidsUnrepresentablePoolUnitIntermediate() {
	offerAmount := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	offerCoin := sdk.NewCoin("ausd", offerAmount)

	s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSet{
			"ausd":             math.LegacyOneDec(),
			chain.XDRBaseDenom: math.LegacyNewDec(2),
			"akrw":             math.LegacyOneDec(),
		}, nil)
	// Zero on both legs keeps the assertion about the conversion route rather
	// than about the spread taken off it.
	s.seedTobinTaxOverride("ausd", math.LegacyZeroDec())
	s.seedTobinTaxOverride("akrw", math.LegacyZeroDec())

	res, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
		OfferCoin: offerCoin.String(),
		AskDenom:  "akrw",
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoin("akrw", offerAmount), res.SwapCoin)
}

func (s *KeeperTestSuite) TestQuerySwapOutcome() {
	tests := []struct {
		name             string
		offerRate        math.LegacyDec
		askRate          math.LegacyDec
		tobinTax         math.LegacyDec
		expectedSwapCoin sdk.Coin
		expectedSwapFee  sdk.DecCoin
		code             codes.Code
		expectErr        string
	}{
		{
			name:             "zero spread returns truncation remainder as fee",
			offerRate:        math.LegacyNewDec(201),
			askRate:          math.LegacyNewDec(2),
			tobinTax:         math.LegacyZeroDec(),
			expectedSwapCoin: sdk.NewCoin("akrw", math.NewInt(100)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("akrw", math.LegacyMustNewDecFromStr("0.5")),
		},
		{
			name:             "positive spread deducts explicit fee",
			offerRate:        math.LegacyNewDec(100),
			askRate:          math.LegacyOneDec(),
			tobinTax:         math.LegacyMustNewDecFromStr("0.2"),
			expectedSwapCoin: sdk.NewCoin("akrw", math.NewInt(80)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("akrw", math.LegacyNewDec(20)),
		},
		{
			name:             "truncation remainder is folded into fee",
			offerRate:        math.LegacyNewDec(2011),
			askRate:          math.LegacyNewDec(20),
			tobinTax:         math.LegacyMustNewDecFromStr("0.1"),
			expectedSwapCoin: sdk.NewCoin("akrw", math.NewInt(90)),
			expectedSwapFee:  sdk.NewDecCoinFromDec("akrw", math.LegacyMustNewDecFromStr("10.55")),
		},
		{
			name:      "zero swap coin returns invalid argument",
			offerRate: math.LegacyOneDec(),
			askRate:   math.LegacyNewDec(2),
			tobinTax:  math.LegacyZeroDec(),
			code:      codes.InvalidArgument,
			expectErr: "zero swap coin",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.setupQuerySwapMocks(tc.offerRate, tc.askRate, tc.tobinTax)

			res, err := s.queryClient.Swap(s.ctx, &types.QuerySwapRequest{
				OfferCoin: "1ausd",
				AskDenom:  "akrw",
			})
			if tc.expectErr != "" {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))
				s.Require().ErrorContains(err, tc.expectErr)
				return
			}

			s.Require().NoError(err)
			s.Require().NotNil(res)
			s.Require().Equal(tc.expectedSwapCoin, res.SwapCoin)
			s.Require().Equal(tc.expectedSwapFee.Denom, res.SwapFee.Denom)
			s.Require().True(tc.expectedSwapFee.Amount.Equal(res.SwapFee.Amount))
		})
	}
}

func (s *KeeperTestSuite) TestQueryPool() {
	tests := []struct {
		name  string
		delta math.LegacyDec
	}{
		{
			name:  "zero delta",
			delta: math.LegacyZeroDec(),
		},
		{
			name:  "positive delta",
			delta: math.LegacyNewDec(98765),
		},
		{
			name:  "negative delta",
			delta: math.LegacyNewDec(-12345),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := s.keeper.ArkPoolDelta.Set(s.ctx, tc.delta)
			s.Require().NoError(err)

			res, err := s.queryClient.Pool(s.ctx, &types.QueryPoolRequest{})
			s.Require().NoError(err)
			s.Require().NotNil(res)
			s.Require().True(tc.delta.Equal(res.ArkPoolDelta))
			// The depth arrives beside the delta it gives meaning to, so the
			// response is a complete pool snapshot on its own.
			s.Require().Equal(types.DefaultConversionPolicy().BasePool, res.BasePool)
		})
	}
}

// TestQueryTobinTax covers rate resolution: an override governs, its absence
// inherits the default, and a zero override is honoured rather than read as no
// entry at all — the one rate where a falsy value could be mistaken for absence
// and silently answer the nonzero default.
// Each case works on its own denomination, so none depends on another's state.
func (s *KeeperTestSuite) TestQueryTobinTax() {
	tests := []struct {
		name       string
		override   *math.LegacyDec
		denom      string
		code       codes.Code
		expectRate math.LegacyDec
	}{
		{
			name:       "no override inherits the default",
			denom:      chain.USDBaseDenom,
			expectRate: types.DefaultTobinTax,
		},
		{
			name:       "override governs",
			denom:      chain.KRWBaseDenom,
			override:   ptr(math.LegacyNewDecWithPrec(5, 2)),
			expectRate: math.LegacyNewDecWithPrec(5, 2),
		},
		{
			name:       "zero override is honoured, not read as absent",
			denom:      chain.MXNBaseDenom,
			override:   ptr(math.LegacyZeroDec()),
			expectRate: math.LegacyZeroDec(),
		},
		{
			name:  "numeraire is never priced",
			denom: chain.NoahBaseDenom,
			code:  codes.InvalidArgument,
		},
		{
			name:  "denom outside the priced charset",
			denom: "uusd",
			code:  codes.InvalidArgument,
		},
		{
			name:  "empty denom",
			denom: "",
			code:  codes.InvalidArgument,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.override != nil {
				s.Require().NoError(
					s.keeper.TobinTaxOverrides.Set(s.ctx, tc.denom, *tc.override),
				)
			}

			res, err := s.queryClient.TobinTax(s.ctx, &types.QueryTobinTaxRequest{
				Denom: tc.denom,
			})
			if tc.code != codes.OK {
				s.Require().Error(err)
				s.Require().Equal(tc.code, status.Code(err))

				return
			}

			s.Require().NoError(err)
			s.Require().True(
				tc.expectRate.Equal(res.TobinTax),
				"expected %s, got %s", tc.expectRate, res.TobinTax,
			)
		})
	}
}

// TestQueryTobinTaxOverrides asserts the order is by key: genesis export reads
// the same walk, so a non-deterministic order there would produce a different
// export on every node. The empty case is asserted only as empty — proto3 omits
// an empty repeated field, so the keeper's non-nil slice arrives as null however
// it was built, and the distinction is only observable in-process.
func (s *KeeperTestSuite) TestQueryTobinTaxOverrides() {
	res, err := s.queryClient.TobinTaxOverrides(s.ctx, &types.QueryTobinTaxOverridesRequest{})
	s.Require().NoError(err)
	s.Require().Empty(res.TobinTaxOverrides)

	// Seeded out of order, so the response order can only come from the walk.
	seeded := []types.TobinTaxOverride{
		{Denom: chain.USDBaseDenom, TobinTax: math.LegacyNewDecWithPrec(1, 2)},
		{Denom: chain.MXNBaseDenom, TobinTax: math.LegacyNewDecWithPrec(2, 2)},
		{Denom: chain.KRWBaseDenom, TobinTax: math.LegacyNewDecWithPrec(3, 2)},
	}
	for _, override := range seeded {
		s.Require().NoError(
			s.keeper.TobinTaxOverrides.Set(s.ctx, override.Denom, override.TobinTax),
		)
	}

	res, err = s.queryClient.TobinTaxOverrides(s.ctx, &types.QueryTobinTaxOverridesRequest{})
	s.Require().NoError(err)
	s.Require().Equal([]types.TobinTaxOverride{
		{Denom: chain.KRWBaseDenom, TobinTax: math.LegacyNewDecWithPrec(3, 2)},
		{Denom: chain.MXNBaseDenom, TobinTax: math.LegacyNewDecWithPrec(2, 2)},
		{Denom: chain.USDBaseDenom, TobinTax: math.LegacyNewDecWithPrec(1, 2)},
	}, res.TobinTaxOverrides)
}

func ptr[T any](value T) *T {
	return &value
}

// setupQuerySwapMocks pins both the rates and the spread of a stable pair. Both
// legs are overridden to the same rate so the wider-of-two rule cannot influence
// the arithmetic under test; the rule itself is covered in the swap tests.
func (s *KeeperTestSuite) setupQuerySwapMocks(offerRate math.LegacyDec, askRate math.LegacyDec, tobinTax math.LegacyDec) {
	s.oracleKeeper.EXPECT().GetRateSet(s.ctx, "ausd", "akrw").
		Return(oracletypes.RateSet{
			"ausd":             offerRate,
			chain.XDRBaseDenom: math.LegacyOneDec(),
			"akrw":             askRate,
		}, nil)
	s.seedTobinTaxOverride("ausd", tobinTax)
	s.seedTobinTaxOverride("akrw", tobinTax)
}

func (s *KeeperTestSuite) TestQueryConversionPolicy() {
	resized := types.DefaultConversionPolicy()
	resized.BasePool = xdrBasePool(math.LegacyNewDec(777))
	resized.PoolRecoveryPeriod = 99
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, resized))

	res, err := s.queryClient.ConversionPolicy(s.ctx, &types.QueryConversionPolicyRequest{})
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Require().Equal(resized, res.ConversionPolicy)
}

func (s *KeeperTestSuite) TestQueryConversionMandate() {
	// The disabled sentinel the suite seeds is never active, whatever the
	// height, so an operator can tell "no committee" from "committee waiting".
	res, err := s.queryClient.ConversionMandate(s.ctx, &types.QueryConversionMandateRequest{})
	s.Require().NoError(err)
	s.Require().True(res.Mandate.IsDisabled())
	s.Require().False(res.Active)

	appointment := s.seedLiveConversionMandate()

	tests := []struct {
		name         string
		height       int64
		expectActive bool
	}{
		{name: "before activation", height: capacityActivation - 1},
		{name: "at activation", height: capacityActivation, expectActive: true},
		{name: "last active height", height: capacityExpiry - 1, expectActive: true},
		{name: "at expiry", height: capacityExpiry},
	}

	// The window is read from the block height, and the suite's gRPC helper
	// captured its context before these heights existed, so this goes through
	// the query server directly to exercise the real half-open comparison.
	queryServer := keeper.NewQueryServerImpl(s.keeper)
	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.setCapacityHeight(tc.height)
			res, err := queryServer.ConversionMandate(s.ctx, &types.QueryConversionMandateRequest{})
			s.Require().NoError(err)
			s.Require().Equal(appointment, res.Mandate)
			s.Require().Equal(tc.expectActive, res.Active)
		})
	}
}
