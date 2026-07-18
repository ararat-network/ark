package keeper_test

import (
	"math/big"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "ark/pkg/chain"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestComputeTaxAppliesCapPerMessageInput() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, math.NewInt(50)))
	s.expectTaxableDenoms(chain.MicroUSDDenom)

	msgs := []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 700))},
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 700))},
	}
	tax, err := s.keeper.ComputeTax(s.ctx, msgs)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)), tax)
}

func (s *KeeperTestSuite) TestComputeTaxSupportsMultiSendAndMarketSend() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, math.NewInt(1_000)))
	s.expectTaxableDenoms(chain.MicroUSDDenom)

	msgs := []sdk.Msg{
		&banktypes.MsgMultiSend{Inputs: []banktypes.Input{
			{Address: sourceA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100))},
			{Address: sourceB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 200))},
		}},
		&markettypes.MsgSwapSend{FromAddress: sourceA, OfferCoin: sdk.NewInt64Coin(chain.MicroUSDDenom, 300)},
		// Direct swaps are intentionally exempt because Market already charges
		// the conversion spread.
		&markettypes.MsgSwap{Trader: sourceA, OfferCoin: sdk.NewInt64Coin(chain.MicroUSDDenom, 10_000)},
	}
	tax, err := s.keeper.ComputeTax(s.ctx, msgs)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 60)), tax)
}

func (s *KeeperTestSuite) TestComputeTaxAppliesCapPerMultiSendInput() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	recipientA := authtypes.NewModuleAddress("tax-recipient-a").String()
	recipientB := authtypes.NewModuleAddress("tax-recipient-b").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, math.NewInt(50)))
	s.expectTaxableDenoms(chain.MicroUSDDenom)

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{
			{Address: sourceA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 700))},
			{Address: sourceB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 700))},
		},
		Outputs: []banktypes.Output{
			{Address: recipientA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 300))},
			{Address: recipientB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 1_100))},
		},
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)), tax)
}

func (s *KeeperTestSuite) TestComputeTaxIgnoresUnconfiguredDenomWithStaleTaxCap() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, math.NewInt(10)))
	s.expectTaxableDenoms(chain.MicroSDRDenom)

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100))},
	})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
}

func (s *KeeperTestSuite) TestComputeTaxFailsClosedForConfiguredDenomWithoutTaxCap() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.expectTaxableDenoms(chain.MicroUSDDenom)

	_, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100))},
	})
	s.Require().ErrorIs(err, types.ErrTaxCapUnavailable)
}

func (s *KeeperTestSuite) TestBuildTaxCapsUsesOneSnapshot() {
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroUSDDenom,
		chain.MicroSDRDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroSDRDenom: math.LegacyNewDec(2),
		chain.MicroUSDDenom: math.LegacyOneDec(),
	}, nil)

	caps, err := s.keeper.BuildTaxCaps(s.ctx, params)
	s.Require().NoError(err)
	s.Require().Equal([]types.TaxCap{
		{Denom: chain.MicroUSDDenom, TaxCap: math.NewInt(500_000)},
		{Denom: chain.MicroSDRDenom, TaxCap: math.NewInt(1_000_000)},
	}, caps)
}

func (s *KeeperTestSuite) TestComputeTaxRejectsMalformedMessagesWhenDisabled() {
	var send *banktypes.MsgSend
	_, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{send})
	s.Require().ErrorIs(err, types.ErrInvalidTaxMessage)
}

func (s *KeeperTestSuite) TestComputeTaxReturnsZeroWithoutOracleLookupWhenDisabled() {
	source := authtypes.NewModuleAddress("tax-source").String()
	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		FromAddress: source,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
}

func (s *KeeperTestSuite) TestComputeTaxRejectsMalformedNestedMessage() {
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))

	badSwapSend := &markettypes.MsgSwapSend{
		OfferCoin: sdk.Coin{Denom: "", Amount: math.OneInt()},
	}
	badAny, err := codectypes.NewAnyWithValue(badSwapSend)
	s.Require().NoError(err)

	_, err = s.keeper.ComputeTax(s.ctx, []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{badAny}}})
	s.Require().ErrorContains(err, "invalid taxable coins")
}

func (s *KeeperTestSuite) TestComputeTaxRejectsAuthzAnyWithoutCachedMessage() {
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))

	_, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&authz.MsgExec{
		Msgs: []*codectypes.Any{{TypeUrl: "/ark.market.v1.MsgSwapSend"}},
	}})
	s.Require().ErrorContains(err, "not a sdk.MsgRequest")
}

func (s *KeeperTestSuite) TestComputeTaxRejectsTypedNilMessages() {
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))

	var send *banktypes.MsgSend
	_, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{send})
	s.Require().ErrorContains(err, "nil bank send message")
}

func (s *KeeperTestSuite) TestComputeTaxRecursesThroughAuthzAndFiltersDenoms() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, math.NewInt(1_000)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroKRWDenom, math.NewInt(1_000)))

	pack := func(msg sdk.Msg) *codectypes.Any {
		packed, err := codectypes.NewAnyWithValue(msg)
		s.Require().NoError(err)
		return packed
	}
	tests := []struct {
		name string
		msgs func() []sdk.Msg
		want sdk.Coins
	}{
		{
			name: "single nested send filters non-taxable principal",
			msgs: func() []sdk.Msg {
				return []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{pack(&banktypes.MsgSend{
					FromAddress: sourceA,
					Amount: sdk.NewCoins(
						sdk.NewInt64Coin("uatom", 900),
						sdk.NewInt64Coin(chain.MicroUSDDenom, 100),
					),
				})}}}
			},
			want: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 10)),
		},
		{
			name: "multiply nested messages aggregate all supported principals",
			msgs: func() []sdk.Msg {
				inner := &authz.MsgExec{Msgs: []*codectypes.Any{
					pack(&banktypes.MsgSend{FromAddress: sourceA, Amount: sdk.NewCoins(
						sdk.NewInt64Coin("uatom", 900),
						sdk.NewInt64Coin(chain.MicroUSDDenom, 100),
					)}),
					pack(&banktypes.MsgMultiSend{Inputs: []banktypes.Input{
						{Address: sourceA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 200))},
						{Address: sourceB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroKRWDenom, 300))},
					}}),
				}}
				return []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{
					pack(inner),
					pack(&markettypes.MsgSwapSend{FromAddress: sourceA, OfferCoin: sdk.NewInt64Coin(chain.MicroUSDDenom, 400)}),
					pack(&markettypes.MsgSwap{Trader: sourceA, OfferCoin: sdk.NewInt64Coin(chain.MicroUSDDenom, 10_000)}),
				}}}
			},
			want: sdk.NewCoins(
				sdk.NewInt64Coin(chain.MicroKRWDenom, 30),
				sdk.NewInt64Coin(chain.MicroUSDDenom, 70),
			),
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.expectTaxableDenoms(chain.MicroUSDDenom, chain.MicroKRWDenom)
			tax, err := s.keeper.ComputeTax(s.ctx, test.msgs())
			s.Require().NoError(err)
			s.Require().Equal(test.want, tax)
		})
	}
}

func (s *KeeperTestSuite) TestComputeTaxReturnsErrorWhenAggregateIsOutOfRange() {
	maxAmount := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), math.MaxBitLen),
		big.NewInt(1),
	)
	maxInt := math.NewIntFromBigInt(maxAmount)
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyOneDec()
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroUSDDenom, maxInt))
	s.expectTaxableDenoms(chain.MicroUSDDenom)
	source := authtypes.NewModuleAddress("tax-source").String()
	msg := func() sdk.Msg {
		return &banktypes.MsgSend{
			FromAddress: source,
			Amount:      sdk.NewCoins(sdk.NewCoin(chain.MicroUSDDenom, maxInt)),
		}
	}

	_, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{msg(), msg()})
	s.Require().ErrorIs(err, types.ErrTaxOutOfRange)
}

func (s *KeeperTestSuite) expectTaxableDenoms(denoms ...string) {
	tobinTaxes := make(oracletypes.TobinTaxes, len(denoms))
	for i, denom := range denoms {
		tobinTaxes[i] = oracletypes.TobinTax{Denom: denom}
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil)
}
