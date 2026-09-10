package keeper_test

import (
	"errors"
	"math/big"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	protov2 "google.golang.org/protobuf/proto"

	bankv1beta1 "cosmossdk.io/api/cosmos/bank/v1beta1"
	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
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
			name: "economic policy",
			call: func() error {
				_, err := server.EconomicPolicy(s.ctx, nil)
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
			name: "economic mandate",
			call: func() error {
				_, err := server.EconomicMandate(s.ctx, nil)
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
	s.Require().NoError(s.keeper.Params.Set(s.ctx, treasurytypes.DefaultParams()))

	response, err := keeper.NewQueryServerImpl(s.keeper).Params(
		s.ctx,
		&treasurytypes.QueryParamsRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(treasurytypes.DefaultParams(), response.Params)
}

func (s *KeeperTestSuite) TestQueryEconomicPolicy() {
	response, err := keeper.NewQueryServerImpl(s.keeper).EconomicPolicy(
		s.ctx,
		&treasurytypes.QueryEconomicPolicyRequest{},
	)
	s.Require().NoError(err)
	s.Require().True(treasurytypes.DefaultEconomicPolicy().Equal(response.Policy))
}

func (s *KeeperTestSuite) TestQueryEconomicMandate() {
	server := keeper.NewQueryServerImpl(s.keeper)
	response, err := server.EconomicMandate(
		s.ctx,
		&treasurytypes.QueryEconomicMandateRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(treasurytypes.DefaultEconomicMandate(), response.Mandate)
	s.False(response.Active)
}

func (s *KeeperTestSuite) TestQueryTaxCap() {
	params := treasurytypes.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(100)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyOneDec(),
	}))
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
			name:     "not found",
			request:  &treasurytypes.QueryTaxCapRequest{Denom: chain.XDRBaseDenom},
			wantCode: codes.NotFound,
		},
		{
			// The numeraire's entry never derives a cap: NOAH is not tax base.
			name:     "noah excluded despite its entry",
			request:  &treasurytypes.QueryTaxCapRequest{Denom: chain.NoahBaseDenom},
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

	// The uncapped sentinel is the zero reference, derived through the same
	// read.
	s.Run("uncapped", func() {
		params.ReferenceTaxCap = math.ZeroInt()
		s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
		response, err := server.TaxCap(s.ctx, &treasurytypes.QueryTaxCapRequest{Denom: chain.USDBaseDenom})
		s.Require().NoError(err)
		s.Require().True(response.TaxCap.IsZero())
	})
}

func (s *KeeperTestSuite) TestQueryTaxCaps() {
	params := treasurytypes.DefaultParams()
	params.ReferenceTaxCap = math.NewInt(100)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.KRWBaseDenom,
		Factor: math.LegacyNewDec(2),
	}))
	// NOAH's entry rides in the table for fee pricing and must not surface
	// here: the cap map is the tax base.
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

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

func (s *KeeperTestSuite) TestQueryConversionFactor() {
	entry := treasurytypes.ConversionFactor{
		Denom:         chain.USDBaseDenom,
		Factor:        math.LegacyNewDec(2),
		DerivedHeight: 7,
	}
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, entry))
	server := keeper.NewQueryServerImpl(s.keeper)

	response, err := server.ConversionFactor(s.ctx, &treasurytypes.QueryConversionFactorRequest{
		Denom: chain.USDBaseDenom,
	})
	s.Require().NoError(err)
	s.Require().Equal(entry, response.ConversionFactor)

	_, err = server.ConversionFactor(s.ctx, &treasurytypes.QueryConversionFactorRequest{
		Denom: chain.KRWBaseDenom,
	})
	s.Require().Equal(codes.NotFound, status.Code(err))
}

func (s *KeeperTestSuite) TestQueryConversionFactors() {
	usd := treasurytypes.ConversionFactor{Denom: chain.USDBaseDenom, Factor: math.LegacyOneDec(), DerivedHeight: 3}
	krw := treasurytypes.ConversionFactor{Denom: chain.KRWBaseDenom, Factor: math.LegacyNewDec(2), DerivedHeight: 5}
	// The numeraire's cross lists like any entry — the factor table is the
	// pricing surface, and only the tax reads exclude NOAH.
	noah := treasurytypes.ConversionFactor{
		Denom:         chain.NoahBaseDenom,
		Factor:        math.LegacyMustNewDecFromStr("0.25"),
		DerivedHeight: 6,
	}
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, usd.Denom, usd))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, krw.Denom, krw))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, noah.Denom, noah))

	response, err := keeper.NewQueryServerImpl(s.keeper).ConversionFactors(
		s.ctx,
		&treasurytypes.QueryConversionFactorsRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal([]treasurytypes.ConversionFactor{krw, noah, usd}, response.ConversionFactors)
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
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), response.TaxBase)
}

// TestQueryComputeTaxBaseSumsPrincipal pins tax_base: every input summed per
// denomination, taxed or not — NOAH is never taxed and counts all the same.
func (s *KeeperTestSuite) TestQueryComputeTaxBaseSumsPrincipal() {
	from := sdk.AccAddress{1}
	to := sdk.AccAddress{2}
	var messages []*codectypes.Any
	for _, amount := range []sdk.Coins{
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100), sdk.NewInt64Coin(chain.NoahBaseDenom, 7)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)),
	} {
		message, err := codectypes.NewAnyWithValue(&banktypes.MsgSend{
			FromAddress: from.String(),
			ToAddress:   to.String(),
			Amount:      amount,
		})
		s.Require().NoError(err)
		messages = append(messages, message)
	}

	response, err := keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: messages},
	)
	s.Require().NoError(err)
	s.Require().Equal(
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 7), sdk.NewInt64Coin(chain.USDBaseDenom, 150)),
		response.TaxBase,
	)
}

// TestQueryComputeTaxDecodesForItself checks uncached wire Any messages decode through the
// registered codec, including dynamically constructed client messages.
func (s *KeeperTestSuite) TestQueryComputeTaxDecodesForItself() {
	from := sdk.AccAddress{1}
	to := sdk.AccAddress{2}
	dynamic, err := protov2.Marshal(&bankv1beta1.MsgSend{
		FromAddress: from.String(),
		ToAddress:   to.String(),
		Amount:      []*basev1beta1.Coin{{Denom: chain.USDBaseDenom, Amount: "100"}},
	})
	s.Require().NoError(err)

	response, err := keeper.NewQueryServerImpl(s.keeper).ComputeTax(
		s.ctx,
		&treasurytypes.QueryComputeTaxRequest{Messages: []*codectypes.Any{{
			TypeUrl: sdk.MsgTypeURL(&banktypes.MsgSend{}),
			Value:   dynamic,
		}}},
	)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), response.TaxBase)
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
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
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
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
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
	maxParams := treasurytypes.DefaultParams()
	maxParams.TransferTaxRate = math.LegacyOneDec()
	maxParams.ReferenceTaxCap = maxInt
	s.Require().NoError(s.keeper.Params.Set(s.ctx, maxParams))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))
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
	// Params carry the rate, so they are the first read the calculator makes.
	s.Require().NoError(s.keeper.Params.Remove(s.ctx))
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
	// Treasury reads each operator's recognised capital. Claims reports 33 from custody 40 less
	// reservations 7; reservation details belong to its Balance query.
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

func (s *KeeperTestSuite) TestQueryFundStatusComputesTargetsFromRecognisedLiability() {
	policy := treasurytypes.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
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

// TestQueryFundStatusAlwaysAnswersWhenValuationIncomplete checks incomplete pricing returns
// disclosed liability gaps and real balances beside zero targets.
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
			policy := treasurytypes.DefaultEconomicPolicy()
			policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
			s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
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
				sdk.Coins{sdk.NewInt64Coin(chain.KRWBaseDenom, 40)},
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

// TestQueryExposureStatusReportsStoredRisk checks stored-state reporting without supply, balance,
// or registry reads.
func (s *KeeperTestSuite) TestQueryExposureStatusReportsStoredRisk() {
	state := treasurytypes.DefaultExposureState()
	// A variance of one annualises to the square root of a year in blocks,
	// which is the bound the sample clamp buys.
	state.VolatilityVariance = math.LegacyOneDec()
	state.FlowPressure = math.LegacyNewDec(12)
	state.LiabilityRatio = math.LegacyMustNewDecFromStr("0.4")
	state.FlowRatio = math.LegacyMustNewDecFromStr("0.05")
	state.Multiplier = math.LegacyMustNewDecFromStr("1.5")
	state.LastRefreshHeight = 7
	s.Require().NoError(s.keeper.ExposureState.Set(s.ctx, state))
	s.Require().NoError(s.keeper.ExposureRefreshPending.Set(s.ctx, true))

	response, err := keeper.NewQueryServerImpl(s.keeper).ExposureStatus(
		s.ctx,
		&treasurytypes.QueryExposureStatusRequest{},
	)
	s.Require().NoError(err)
	s.Require().Equal(state, response.ExposureState)
	s.Require().True(response.RefreshPending)

	expected, err := math.LegacyNewDec(int64(chain.BlocksPerYear)).ApproxSqrt()
	s.Require().NoError(err)
	s.Require().Equal(expected, response.AnnualisedVolatility)
}

// TestQueryFundStatusReportsScaledTargetsAndMultiplier checks target values reconcile with the
// reported liability bases, policy ratios, and exposure multiplier.
func (s *KeeperTestSuite) TestQueryFundStatusReportsScaledTargetsAndMultiplier() {
	policy := treasurytypes.DefaultEconomicPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.5")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.25")
	s.Require().NoError(s.keeper.EconomicPolicy.Set(s.ctx, policy))
	s.setMultiplier("2")
	s.setAssets(chain.USDBaseDenom)
	s.bankKeeper.EXPECT().GetSupply(s.ctx, chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100))
	s.setRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyOneDec()})
	s.bankKeeper.EXPECT().GetBalance(s.ctx, gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	response, err := keeper.NewQueryServerImpl(s.keeper).FundStatus(
		s.ctx,
		&treasurytypes.QueryFundStatusRequest{},
	)
	s.Require().NoError(err)

	// The liability reported is the raw aggregate — the multiplier scales the
	// targets, never the figure the draw divides by.
	s.Require().Equal(
		sdk.NewDecCoinFromDec(chain.NoahBaseDenom, math.LegacyNewDec(100)),
		response.NominalLiability,
	)
	s.Require().Equal(math.LegacyNewDec(2), response.ExposureMultiplier)
	// Each target is its ratio against a doubled basis, and the proportions
	// between the three are the voted ones.
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), response.RedemptionBufferTarget)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 50), response.StrategicReserveTarget)
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 50), response.InsuranceTarget)
	// The net family scales on the same multiplier, so the two stay comparable.
	s.Require().Equal(sdk.NewInt64Coin(chain.NoahBaseDenom, 100), response.RedemptionBufferNetTarget)
}

// TestQueryGasPrice pins the single-row read over both factor sources
// and the refusal for a denomination with no cross.
func (s *KeeperTestSuite) TestQueryGasPrice() {
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, treasurytypes.ConversionFactor{
		Denom:         chain.USDBaseDenom,
		Factor:        math.LegacyNewDec(2),
		DerivedHeight: 5,
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, treasurytypes.ConversionFactor{
		Denom:         chain.NoahBaseDenom,
		Factor:        math.LegacyMustNewDecFromStr("0.25"),
		DerivedHeight: 6,
	}))
	server := keeper.NewQueryServerImpl(s.keeper)

	reference, err := server.GasPrice(s.ctx, &treasurytypes.QueryGasPriceRequest{Denom: chain.XDRBaseDenom})
	s.Require().NoError(err)
	s.Require().Equal(treasurytypes.GasPrice{
		Denom:    chain.XDRBaseDenom,
		GasPrice: testMinBaseGasPrice,
	}, reference.GasPrice)

	member, err := server.GasPrice(s.ctx, &treasurytypes.QueryGasPriceRequest{Denom: chain.USDBaseDenom})
	s.Require().NoError(err)
	s.Require().Equal(treasurytypes.GasPrice{
		Denom:         chain.USDBaseDenom,
		GasPrice:      math.LegacyMustNewDecFromStr("0.2"),
		DerivedHeight: 5,
	}, member.GasPrice)

	noah, err := server.GasPrice(s.ctx, &treasurytypes.QueryGasPriceRequest{Denom: chain.NoahBaseDenom})
	s.Require().NoError(err)
	s.Require().Equal(treasurytypes.GasPrice{
		Denom:         chain.NoahBaseDenom,
		GasPrice:      math.LegacyMustNewDecFromStr("0.025"),
		DerivedHeight: 6,
	}, noah.GasPrice)

	_, err = server.GasPrice(s.ctx, &treasurytypes.QueryGasPriceRequest{Denom: chain.KRWBaseDenom})
	s.Require().ErrorContains(err, "not an accepted fee denomination")
}

// TestQueryGasPricesSheet pins the sheet's shape: the reference row in its
// own field — the base price at the identity factor, never a list entry even
// as a member — and every other accepted denomination, NOAH among them, in
// denomination order.
func (s *KeeperTestSuite) TestQueryGasPricesSheet() {
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.XDRBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.XDRBaseDenom,
		Factor: math.LegacyOneDec(),
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.KRWBaseDenom, treasurytypes.ConversionFactor{
		Denom:         chain.KRWBaseDenom,
		Factor:        math.LegacyNewDec(4),
		DerivedHeight: 3,
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, treasurytypes.ConversionFactor{
		Denom:         chain.NoahBaseDenom,
		Factor:        math.LegacyMustNewDecFromStr("0.25"),
		DerivedHeight: 6,
	}))
	server := keeper.NewQueryServerImpl(s.keeper)

	response, err := server.GasPrices(s.ctx, &treasurytypes.QueryGasPricesRequest{})
	s.Require().NoError(err)
	s.Require().Equal(
		treasurytypes.GasPrice{Denom: chain.XDRBaseDenom, GasPrice: testMinBaseGasPrice},
		response.ReferenceGasPrice,
	)
	s.Require().Equal([]treasurytypes.GasPrice{
		{Denom: chain.KRWBaseDenom, GasPrice: math.LegacyMustNewDecFromStr("0.4"), DerivedHeight: 3},
		{Denom: chain.NoahBaseDenom, GasPrice: math.LegacyMustNewDecFromStr("0.025"), DerivedHeight: 6},
	}, response.GasPrices)
}
