package keeper_test

import (
	"errors"
	"math/big"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"ark/pkg/chain"
	assettypes "ark/x/asset/types"
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
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(100)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.KRWBaseDenom, math.ZeroInt()))
	server := keeper.NewQueryServerImpl(s.keeper)
	tests := []struct {
		name     string
		request  *treasurytypes.QueryTaxCapRequest
		wantCode codes.Code
		wantCap  math.Int
	}{
		{
			name:    "found",
			request: &treasurytypes.QueryTaxCapRequest{Denom: chain.USDBaseDenom},
			wantCap: math.NewInt(100),
		},
		{
			name:    "uncapped",
			request: &treasurytypes.QueryTaxCapRequest{Denom: chain.KRWBaseDenom},
			wantCap: math.ZeroInt(),
		},
		{
			name:     "not found",
			request:  &treasurytypes.QueryTaxCapRequest{Denom: chain.SDRBaseDenom},
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
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(100)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.KRWBaseDenom, math.NewInt(200)))

	response, err := keeper.NewQueryServerImpl(s.keeper).TaxCaps(
		s.ctx,
		&treasurytypes.QueryTaxCapsRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal([]treasurytypes.TaxCap{
		{Denom: chain.KRWBaseDenom, TaxCap: math.NewInt(200)},
		{Denom: chain.USDBaseDenom, TaxCap: math.NewInt(100)},
	}, response.TaxCaps)
}

func (s *KeeperTestSuite) TestQueryComputeTax() {
	from := sdk.AccAddress{1}
	to := sdk.AccAddress{2}
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		FromAddress: from.String(),
		ToAddress:   to.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
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

// TestQueryComputeTaxAnswersZeroForMissingCap pins the query to the same
// contract as the ante path: a denomination with no cap quotes zero tax rather
// than an error, so a client cannot be told a transfer is impossible when the
// chain would accept it.
func (s *KeeperTestSuite) TestQueryComputeTaxAnswersZeroForMissingCap() {
	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
	})
	s.Require().NoError(err)

	response, err := keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{message}},
	)
	s.Require().NoError(err)
	s.Require().True(response.Tax.IsZero())
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
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, maxInt))
	message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, maxInt)),
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
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
	})
	s.Require().NoError(err)

	_, err = keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{message}},
	)
	s.Require().Equal(codes.Internal, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryFundStatus() {
	s.setAssets()
	// Both committee-operated funds report through their own keeper rather than
	// through a Bank stub: Treasury asks each operator, never the account.
	// Insurance holds 40 with 7 encumbered by pending claims, which x/claims
	// reports as 33 recognised — the encumbered 7 is claims state, served by its
	// own Query/ClaimsMandate, and Treasury never sees it.
	s.setInsuranceRecognised(33)
	s.setReserveRecognised(30)
	balances := map[string]int64{
		treasurytypes.SubsidyPoolName:      10,
		treasurytypes.RedemptionBufferName: 20,
	}
	for moduleName, amount := range balances {
		address := authtypes.NewModuleAddress(moduleName)
		s.bankKeeper.EXPECT().GetBalance(s.ctx, address, chain.NoahBaseDenom).
			Return(sdk.NewInt64Coin(chain.NoahBaseDenom, amount))
	}

	response, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
		s.ctx,
		&treasurytypes.QueryFundStatusRequest{},
	)
	s.Require().NoError(err)
	// An empty registry is a complete valuation of nothing: both exclusion
	// lists empty is what says every recognised liability was valued.
	s.Require().Empty(response.UntrustedSuspendedSupply)
	s.Require().Empty(response.StaleMemberSupply)
	s.Require().True(response.PricedLiability.IsZero())
	s.Require().True(response.SettlementLiability.IsZero())
	s.Require().Empty(response.WrittenOffExposure)
	s.Require().True(response.NominalLiability.IsZero())
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 10), response.SubsidyPoolBalance)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 20), response.RedemptionBufferBalance)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 30), response.StrategicReserveBalance)
	// The reported Insurance balance is what x/claims recognises, not the 40 the
	// module account holds.
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 33), response.InsuranceBalance)
}

func (s *KeeperTestSuite) TestQueryFundStatusComputesTargetsFromRecognizedLiability() {
	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(s.ctx, chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	// Two Bank reads remain: the Subsidy Pool and the Redemption Buffer. Both
	// committee-operated funds answer through their own keeper.
	s.bankKeeper.EXPECT().GetBalance(s.ctx, gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	response, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
		s.ctx,
		&treasurytypes.QueryFundStatusRequest{},
	)
	s.Require().NoError(err)
	s.Require().Empty(response.UntrustedSuspendedSupply)
	s.Require().Empty(response.StaleMemberSupply)
	s.Require().Equal(
		sdk.NewDecCoinFromDec(chain.NoahBaseDenom, math.LegacyNewDec(100)),
		response.NominalLiability,
	)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 50), response.RedemptionBufferTarget)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 25), response.StrategicReserveTarget)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 25), response.InsuranceTarget)
}

// TestQueryFundStatusAlwaysAnswersWhenValuationIncomplete pins the inversion
// of the old contract: incompleteness used to be a FailedPrecondition error,
// which blinded operators during exactly the stress that makes valuation
// incomplete. The query now always answers with the partition that explains
// the gap — real balances beside zero targets that claim nothing.
func (s *KeeperTestSuite) TestQueryFundStatusAlwaysAnswersWhenValuationIncomplete() {
	tests := []struct {
		name      string
		oracleErr error
	}{
		{name: "stale exchange rate", oracleErr: oracletypes.ErrStaleExchangeRate},
		{name: "unknown denom", oracleErr: oracletypes.ErrUnknownDenom},
		{name: "conversion out of range", oracleErr: oracletypes.ErrConversionOutOfRange},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			policy := treasurytypes.DefaultMonetaryPolicy()
			policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
			s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
			s.setAssets(chain.USDBaseDenom)
			s.seedAsset(chain.KRWBaseDenom, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED)
			s.bankKeeper.EXPECT().GetSupply(s.ctx, chain.KRWBaseDenom).
				Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 40))
			s.bankKeeper.EXPECT().GetSupply(s.ctx, chain.USDBaseDenom).
				Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
			s.setRates(oracletypes.RateSet{})
			s.bankKeeper.EXPECT().GetBalance(s.ctx, gomock.Any(), chain.NoahBaseDenom).
				Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 20)).Times(2)

			response, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
				s.ctx,
				&treasurytypes.QueryFundStatusRequest{},
			)
			s.Require().NoError(err)
			s.Require().True(response.PricedLiability.IsZero())
			s.Require().Equal(
				[]sdk.Coin{sdk.NewInt64Coin(chain.KRWBaseDenom, 40)},
				response.UntrustedSuspendedSupply,
			)
			// Zero recognised liability yields zero targets: the report never
			// guesses, while the balances stay real.
			s.Require().True(response.NominalLiability.IsZero())
			s.Require().True(response.RedemptionBufferTarget.IsZero())
			s.Require().True(response.StrategicReserveTarget.IsZero())
			s.Require().True(response.InsuranceTarget.IsZero())
			s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 20), response.RedemptionBufferBalance)
		})
	}
}

func (s *KeeperTestSuite) TestQueryFundStatusClassifiesUnexpectedStateError() {
	s.setAssets(chain.USDBaseDenom)
	// No supply read is stubbed, and none is expected: the partition prices the
	// registry before it asks what is outstanding, so a fold that cannot price
	// fails without reading supply at all.
	s.ratesErr = errors.New("oracle store failure")

	response, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
		s.ctx,
		&treasurytypes.QueryFundStatusRequest{},
	)
	s.Require().Nil(response)
	s.Require().Equal(codes.Internal, status.Code(err))
	s.Require().Equal(
		"getting treasury fund status: pricing aggregate liability: oracle store failure",
		status.Convert(err).Message(),
	)
}

func (s *KeeperTestSuite) TestQueryRewardFundingDoesNotRequireFundValuation() {
	funding := rewardFunding(4, 7, 3, 2)
	s.setRewardFunding(funding)

	response, err := keeper.NewQueryServerImpl(s.keeper).RewardFunding(
		s.ctx,
		&treasurytypes.QueryRewardFundingRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(funding, response.RewardFunding)
}
