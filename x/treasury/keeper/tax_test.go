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
	assettypes "ark/x/asset/types"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestComputeTaxAppliesCapPerMessageInput() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(50)))

	msgs := []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
	}
	tax, err := s.keeper.ComputeTax(s.ctx, msgs)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), tax)
}

func (s *KeeperTestSuite) TestComputeTaxSupportsMultiSendAndMarketSend() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(1_000)))

	msgs := []sdk.Msg{
		&banktypes.MsgMultiSend{Inputs: []banktypes.Input{
			{Address: sourceA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))},
			{Address: sourceB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 200))},
		}},
		&markettypes.MsgSwapSend{FromAddress: sourceA, OfferCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 300)},
		// Direct swaps are intentionally exempt because Market already charges
		// the conversion spread.
		&markettypes.MsgSwap{Trader: sourceA, OfferCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)},
	}
	tax, err := s.keeper.ComputeTax(s.ctx, msgs)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 60)), tax)
}

func (s *KeeperTestSuite) TestComputeTaxAppliesCapPerMultiSendInput() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	recipientA := authtypes.NewModuleAddress("tax-recipient-a").String()
	recipientB := authtypes.NewModuleAddress("tax-recipient-b").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(50)))

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgMultiSend{
		Inputs: []banktypes.Input{
			{Address: sourceA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
			{Address: sourceB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
		},
		Outputs: []banktypes.Output{
			{Address: recipientA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 300))},
			{Address: recipientB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_100))},
		},
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), tax)
}

// TestComputeTaxTaxesDepartedDenomWithKeptCap pins the tax base to the cap set
// rather than to lifecycle status: ausd has left oracle-priced membership, but
// its outstanding supply is still transferable and its kept cap still bounds
// the tax on that transfer.
func (s *KeeperTestSuite) TestComputeTaxTaxesDepartedDenomWithKeptCap() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(10)))
	s.setAssets(chain.SDRBaseDenom)

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))},
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10)), tax)
}

// TestComputeTaxSkipsDenomWithoutTaxCap covers the other direction: a member
// whose cap has not been derived yet is untaxed rather than rejected, so a
// stalled refresh costs revenue instead of blocking transfers.
func (s *KeeperTestSuite) TestComputeTaxSkipsDenomWithoutTaxCap() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.setAssets(chain.USDBaseDenom)

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))},
	})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
}

func (s *KeeperTestSuite) TestComputeTaxTreatsZeroCapAsUncapped() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.ZeroInt()))

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000))},
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), tax)
}

func (s *KeeperTestSuite) TestBuildTaxCapsUsesOneSnapshot() {
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.setAssets(chain.USDBaseDenom, chain.SDRBaseDenom)
	// One capture for the whole membership, in sorted member order. The
	// reference denomination is appended only when it is not already a member,
	// and here it is one.
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyNewDec(2),
		chain.USDBaseDenom: math.LegacyOneDec(),
	}, nil)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(500_000), usdCap)
	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(1_000_000), sdrCap)
}

// TestUpdateParamsRebuildServesOutstandingCadenceRefresh pins the flag clear at
// the message call site. This rebuild derives every member from current inputs,
// which is exactly the work an outstanding cadence refresh was owed, so leaving
// the flag raised would make the next block redo what governance just did.
func (s *KeeperTestSuite) TestUpdateParamsRebuildServesOutstandingCadenceRefresh() {
	s.Require().NoError(s.keeper.TaxCapRefreshPending.Set(s.ctx, true))
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.setAssets(chain.USDBaseDenom, chain.SDRBaseDenom)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	}, nil)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	s.requireTaxCapRefreshPending(false)
}

// TestBuildTaxCapsUsesZeroAsUncappedWithoutRates is the complement of the
// sub-unit flooring above: now that a truncated conversion floors at one, a
// deliberately zero reference cap is the only thing that can leave a derived
// denom uncapped. No rate is captured to do it, so the oracle mock stays
// unprogrammed.
func (s *KeeperTestSuite) TestBuildTaxCapsUsesZeroAsUncappedWithoutRates() {
	current := types.DefaultParams()
	current.ReferenceTaxCap.Amount = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, current))
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.ZeroInt()
	s.setAssets(chain.USDBaseDenom, chain.SDRBaseDenom)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	for _, denom := range []string{chain.USDBaseDenom, chain.SDRBaseDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().True(cap.IsZero())
	}
}

func (s *KeeperTestSuite) TestBuildTaxCapsFloorsSubUnitConversionAtOneUnit() {
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.OneInt()
	s.setAssets(chain.USDBaseDenom, chain.SDRBaseDenom)
	// A one-base-unit cap is worth half a unit of ausd at this pair. Storing
	// the zero it truncates to would read as uncapped, so it floors at one:
	// the tightest ceiling ausd can express, which is what a reference cap
	// this small is asking for.
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyNewDec(2),
		chain.USDBaseDenom: math.LegacyOneDec(),
	}, nil)

	_, err := s.msgServer.UpdateParams(s.ctx, &types.MsgUpdateParams{
		Authority: s.authority,
		Params:    params,
	})
	s.Require().NoError(err)
	usdCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.OneInt(), usdCap)
	sdrCap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(math.OneInt(), sdrCap)
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
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
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
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(1_000)))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.KRWBaseDenom, math.NewInt(1_000)))

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
						sdk.NewInt64Coin("aatom", 900),
						sdk.NewInt64Coin(chain.USDBaseDenom, 100),
					),
				})}}}
			},
			want: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10)),
		},
		{
			name: "multiply nested messages aggregate all supported principals",
			msgs: func() []sdk.Msg {
				inner := &authz.MsgExec{Msgs: []*codectypes.Any{
					pack(&banktypes.MsgSend{FromAddress: sourceA, Amount: sdk.NewCoins(
						sdk.NewInt64Coin("aatom", 900),
						sdk.NewInt64Coin(chain.USDBaseDenom, 100),
					)}),
					pack(&banktypes.MsgMultiSend{Inputs: []banktypes.Input{
						{Address: sourceA, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 200))},
						{Address: sourceB, Coins: sdk.NewCoins(sdk.NewInt64Coin(chain.KRWBaseDenom, 300))},
					}}),
				}}
				return []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{
					pack(inner),
					pack(&markettypes.MsgSwapSend{FromAddress: sourceA, OfferCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 400)}),
					pack(&markettypes.MsgSwap{Trader: sourceA, OfferCoin: sdk.NewInt64Coin(chain.USDBaseDenom, 10_000)}),
				}}}
			},
			want: sdk.NewCoins(
				sdk.NewInt64Coin(chain.KRWBaseDenom, 30),
				sdk.NewInt64Coin(chain.USDBaseDenom, 70),
			),
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
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
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, maxInt))
	source := authtypes.NewModuleAddress("tax-source").String()
	msg := func() sdk.Msg {
		return &banktypes.MsgSend{
			FromAddress: source,
			Amount:      sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, maxInt)),
		}
	}

	_, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{msg(), msg()})
	s.Require().ErrorIs(err, types.ErrTaxOutOfRange)
}

// TestComputeTaxTaxesDistressedDenominations proves lifecycle status is not a
// tax exemption. Suspended, written-off, and retired supply all remain
// transferable — retirement can even leave a residual — so exempting them
// would price distressed money below ordinary money for the one operation
// holders can still perform with it.
func (s *KeeperTestSuite) TestComputeTaxTaxesDistressedDenominations() {
	source := authtypes.NewModuleAddress("tax-source").String()
	statuses := []assettypes.AssetStatus{
		assettypes.AssetStatus_ASSET_STATUS_SUSPENDED,
		assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF,
		assettypes.AssetStatus_ASSET_STATUS_RETIRED,
	}

	for _, status := range statuses {
		s.Run(status.String(), func() {
			policy := types.DefaultMonetaryPolicy()
			policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
			s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
			s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.USDBaseDenom, math.NewInt(1_000)))
			s.seedAsset(chain.USDBaseDenom, status)

			tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
				&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))},
			})
			s.Require().NoError(err)
			s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 10)), tax)
		})
	}
}

// TestComputeTaxSkipsUnregisteredDenomination covers the denominations that
// never enter the cap set at all — anoah, IBC vouchers, anything the asset
// registry does not know — which have no derived ceiling and so are not part
// of the tax base.
func (s *KeeperTestSuite) TestComputeTaxSkipsUnregisteredDenomination() {
	source := authtypes.NewModuleAddress("tax-source").String()
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin("aatom", 100))},
	})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
}
