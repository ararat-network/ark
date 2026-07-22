package keeper_test

import (
	"math/big"

	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	querytypes "github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/keeper"
	treasurytypes "ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestQueryNilRequests() {
	server := keeper.NewQueryServerImpl(s.keeper)
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "params",
			call: func() error {
				_, err := server.Params(s.ctx, nil)
				return err
			},
		},
		{
			name: "monetary policy",
			call: func() error {
				_, err := server.MonetaryPolicy(s.ctx, nil)
				return err
			},
		},
		{
			name: "tax cap",
			call: func() error {
				_, err := server.TaxCap(s.ctx, nil)
				return err
			},
		},
		{
			name: "monetary mandate",
			call: func() error {
				_, err := server.MonetaryMandate(s.ctx, nil)
				return err
			},
		},
		{
			name: "tax caps",
			call: func() error {
				_, err := server.TaxCaps(s.ctx, nil)
				return err
			},
		},
		{
			name: "compute tax",
			call: func() error {
				_, err := server.ComputeTax(s.ctx, nil)
				return err
			},
		},
		{
			name: "fund status",
			call: func() error {
				_, err := server.FundStatus(s.ctx, nil)
				return err
			},
		},
		{
			name: "reward funding",
			call: func() error {
				_, err := server.RewardFunding(s.ctx, nil)
				return err
			},
		},
		{
			name: "claims mandate",
			call: func() error {
				_, err := server.ClaimsMandate(s.ctx, nil)
				return err
			},
		},
		{
			name: "claim",
			call: func() error {
				_, err := server.Claim(s.ctx, nil)
				return err
			},
		},
		{
			name: "claims",
			call: func() error {
				_, err := server.Claims(s.ctx, nil)
				return err
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := tc.call()
			s.Require().Error(err)
			s.Require().Equal(codes.InvalidArgument, status.Code(err))
		})
	}
}

func (s *KeeperTestSuite) TestQueryParams() {
	response, err := keeper.NewQueryServerImpl(s.keeper).Params(
		s.ctx,
		&treasurytypes.QueryParamsRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(treasurytypes.DefaultParams(), response.Params)
}

func (s *KeeperTestSuite) TestQueryMonetaryPolicy() {
	response, err := keeper.NewQueryServerImpl(s.keeper).MonetaryPolicy(
		s.ctx,
		&treasurytypes.QueryMonetaryPolicyRequest{},
	)
	s.Require().NoError(err)
	s.Require().True(treasurytypes.DefaultMonetaryPolicy().Equal(response.Policy))
}

func (s *KeeperTestSuite) TestQueryMonetaryMandate() {
	server := keeper.NewQueryServerImpl(s.keeper)
	response, err := server.MonetaryMandate(
		s.ctx,
		&treasurytypes.QueryMonetaryMandateRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(treasurytypes.DefaultMonetaryMandate(), response.Mandate)
	s.False(response.Active)
}

func (s *KeeperTestSuite) TestQueryTaxCap() {
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, math.NewInt(100)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroKRWDenom, math.ZeroInt()))
	server := keeper.NewQueryServerImpl(s.keeper)
	tests := []struct {
		name     string
		request  *treasurytypes.QueryTaxCapRequest
		wantCode codes.Code
		wantCap  math.Int
	}{
		{
			name:    "found",
			request: &treasurytypes.QueryTaxCapRequest{Denom: chain.MicroUSDDenom},
			wantCap: math.NewInt(100),
		},
		{
			name:    "uncapped",
			request: &treasurytypes.QueryTaxCapRequest{Denom: chain.MicroKRWDenom},
			wantCap: math.ZeroInt(),
		},
		{
			name:     "not found",
			request:  &treasurytypes.QueryTaxCapRequest{Denom: chain.MicroSDRDenom},
			wantCode: codes.NotFound,
		},
		{
			name:     "empty denom",
			request:  &treasurytypes.QueryTaxCapRequest{},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			response, err := server.TaxCap(s.ctx, tc.request)
			if tc.wantCode != codes.OK {
				s.Require().Error(err)
				s.Require().Equal(tc.wantCode, status.Code(err))
				return
			}
			s.Require().NoError(err)
			s.Require().True(tc.wantCap.Equal(response.TaxCap))
		})
	}
}

func (s *KeeperTestSuite) TestQueryTaxCaps() {
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, math.NewInt(100)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroKRWDenom, math.NewInt(200)))

	response, err := keeper.NewQueryServerImpl(s.keeper).TaxCaps(
		s.ctx,
		&treasurytypes.QueryTaxCapsRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal([]treasurytypes.TaxCap{
		{Denom: chain.MicroKRWDenom, TaxCap: math.NewInt(200)},
		{Denom: chain.MicroUSDDenom, TaxCap: math.NewInt(100)},
	}, response.TaxCaps)
}

func (s *KeeperTestSuite) TestQueryComputeTax() {
	from := sdk.AccAddress{1}
	to := sdk.AccAddress{2}
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		FromAddress: from.String(),
		ToAddress:   to.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)),
	})
	s.Require().NoError(err)

	response, err := keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{message}},
	)
	s.Require().NoError(err)
	s.Require().True(response.Tax.IsZero())
}

func (s *KeeperTestSuite) TestQueryComputeTaxRejectsNilMessage() {
	_, err := keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{nil}},
	)
	s.Require().Error(err)
	s.Require().Equal(codes.InvalidArgument, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryComputeTaxClassifiesInvalidTaxMessage() {
	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		Amount: sdk.Coins{{Denom: "", Amount: math.OneInt()}},
	})
	s.Require().NoError(err)

	_, err = keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{message}},
	)
	s.Require().Equal(codes.InvalidArgument, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryComputeTaxClassifiesMissingConfiguredCap() {
	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.expectTaxableDenoms(chain.MicroUSDDenom)
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)),
	})
	s.Require().NoError(err)

	_, err = keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{message}},
	)
	s.Require().Equal(codes.FailedPrecondition, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryComputeTaxClassifiesOutOfRangeTotal() {
	maxAmount := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), math.MaxBitLen),
		big.NewInt(1),
	)
	maxInt := math.NewIntFromBigInt(maxAmount)
	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyOneDec()
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, maxInt))
	s.expectTaxableDenoms(chain.MicroUSDDenom)
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewCoin(chain.MicroUSDDenom, maxInt)),
	})
	s.Require().NoError(err)

	_, err = keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{message, message}},
	)
	s.Require().Equal(codes.OutOfRange, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryComputeTaxClassifiesUnexpectedStateError() {
	s.Require().NoError(s.keeper.MonetaryPolicy.Remove(s.ctx))
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)),
	})
	s.Require().NoError(err)

	_, err = keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{message}},
	)
	s.Require().Equal(codes.Internal, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryFundStatus() {
	s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.NewInt(7)))
	balances := map[string]int64{
		treasurytypes.SubsidyPoolName:      10,
		treasurytypes.RedemptionBufferName: 20,
		treasurytypes.StrategicReserveName: 30,
		treasurytypes.InsuranceName:        40,
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{}, nil)
	for moduleName, amount := range balances {
		address := authtypes.NewModuleAddress(moduleName)
		s.bankKeeper.EXPECT().GetBalance(s.ctx, address, chain.MicroNoahDenom).
			Return(sdk.NewInt64Coin(chain.MicroNoahDenom, amount))
	}

	response, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
		s.ctx,
		&treasurytypes.QueryFundStatusRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewInt64Coin(chain.MicroNoahDenom, 10), response.SubsidyPoolBalance)
	s.Require().Equal(sdk.NewInt64Coin(chain.MicroNoahDenom, 20), response.RedemptionBufferBalance)
	s.Require().Equal(sdk.NewInt64Coin(chain.MicroNoahDenom, 30), response.StrategicReserveBalance)
	s.Require().Equal(sdk.NewInt64Coin(chain.MicroNoahDenom, 40), response.InsuranceBalance)
	s.Require().Equal(sdk.NewInt64Coin(chain.MicroNoahDenom, 7), response.InsuranceReserved)
	s.Require().Equal(sdk.NewInt64Coin(chain.MicroNoahDenom, 33), response.InsuranceUnencumberedBalance)
}

func (s *KeeperTestSuite) TestQueryRewardFundingDoesNotRequireFundValuation() {
	funding := rewardFunding(4, 7, 3, 2, true)
	s.setRewardFunding(funding)

	response, err := keeper.NewQueryServerImpl(s.keeper).RewardFunding(
		s.ctx,
		&treasurytypes.QueryRewardFundingRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(funding, response.RewardFunding)
}

func (s *KeeperTestSuite) TestQueryClaimsMandate() {
	server := keeper.NewQueryServerImpl(s.keeper)
	response, err := server.ClaimsMandate(
		s.ctx,
		&treasurytypes.QueryClaimsMandateRequest{},
	)
	s.Require().NoError(err)
	expectedMandate := treasurytypes.DefaultClaimsMandate()
	s.Require().True(proto.Equal(&response.Mandate, &expectedMandate))
	s.Require().True(response.InsuranceReserved.IsZero())
	s.Require().True(response.AllowanceUsed.IsZero())
	s.Require().True(response.AllowanceRemaining.IsZero())
	s.False(response.Active)

	mandate := treasurytypes.ClaimsMandate{
		Term:                     1,
		Committee:                authtypes.NewModuleAddress("claims-committee").String(),
		ActivationHeight:         10,
		ExpiryHeight:             20,
		CancellationPeriodBlocks: 2,
		CommitteeClaimLimit:      math.NewInt(100),
	}
	s.Require().NoError(s.keeper.ClaimsMandate.Set(s.ctx, mandate))
	s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.NewInt(40)))
	s.setBlockHeight(10)
	response, err = server.ClaimsMandate(s.ctx, &treasurytypes.QueryClaimsMandateRequest{})
	s.Require().NoError(err)
	s.Require().Equal(mandate, response.Mandate)
	s.Require().Equal(math.NewInt(40), response.AllowanceUsed)
	s.Require().Equal(math.NewInt(60), response.AllowanceRemaining)
	s.True(response.Active)
}

func (s *KeeperTestSuite) TestQueryClaim() {
	claim := treasurytypes.Claim{
		ClaimId: 1,
		Amount:  sdk.NewInt64Coin(chain.MicroNoahDenom, 1),
	}
	s.Require().NoError(s.keeper.Claims.Set(s.ctx, claim.ClaimId, claim))
	server := keeper.NewQueryServerImpl(s.keeper)

	response, err := server.Claim(
		s.ctx,
		&treasurytypes.QueryClaimRequest{ClaimId: claim.ClaimId},
	)
	s.Require().NoError(err)
	s.Require().Equal(claim, response.Claim)

	_, err = server.Claim(
		s.ctx,
		&treasurytypes.QueryClaimRequest{ClaimId: 999},
	)
	s.Require().Error(err)
	s.Require().Equal(codes.NotFound, status.Code(err))

	_, err = server.Claim(s.ctx, &treasurytypes.QueryClaimRequest{})
	s.Require().Error(err)
	s.Require().Equal(codes.InvalidArgument, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryClaimsPagination() {
	for _, claimID := range []uint64{1, 2, 3} {
		claim := treasurytypes.Claim{ClaimId: claimID}
		s.Require().NoError(s.keeper.Claims.Set(s.ctx, claimID, claim))
	}
	server := keeper.NewQueryServerImpl(s.keeper)

	first, err := server.Claims(s.ctx, &treasurytypes.QueryClaimsRequest{
		Pagination: &querytypes.PageRequest{Limit: 2, CountTotal: true},
	})
	s.Require().NoError(err)
	s.Require().Equal([]uint64{1, 2}, claimIDs(first.Claims))
	s.Require().Equal(uint64(3), first.Pagination.Total)
	s.Require().NotEmpty(first.Pagination.NextKey)

	second, err := server.Claims(s.ctx, &treasurytypes.QueryClaimsRequest{
		Pagination: &querytypes.PageRequest{Key: first.Pagination.NextKey, Limit: 2},
	})
	s.Require().NoError(err)
	s.Require().Equal([]uint64{3}, claimIDs(second.Claims))
}

func claimIDs(claims []treasurytypes.Claim) []uint64 {
	ids := make([]uint64, len(claims))
	for i, claim := range claims {
		ids[i] = claim.ClaimId
	}
	return ids
}
