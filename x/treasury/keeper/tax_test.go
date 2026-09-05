package keeper_test

import (
	"math/big"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	gmptypes "github.com/cosmos/ibc-go/v11/modules/apps/27-gmp/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v11/modules/core/04-channel/v2/types"

	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestComputeTaxAppliesCapPerMessageInput() {
	source := authtypes.NewModuleAddress("tax-source").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(50))

	msgs := []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
	}
	tax, base, err := s.keeper.ComputeTax(s.ctx, msgs)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), tax)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_400)), base)
}

func (s *KeeperTestSuite) TestComputeTaxSupportsMultiSendAndMarketSend() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(1_000))

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
	tax, _, err := s.keeper.ComputeTax(s.ctx, msgs)
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 60)), tax)
}

// transferPayload builds an ICS-20 payload the transfer module would accept,
// in either encoding it supports, so the tax cases exercise the same decode
// the escrow does.
func transferPayload(encoding, denom, amount, sender string) channeltypesv2.Payload {
	data := ibctransfertypes.NewFungibleTokenPacketData(denom, amount, sender, "receiver", "")
	value := data.GetBytes()
	if encoding == ibctransfertypes.EncodingProtobuf {
		value = ibctransfertypes.ModuleCdc.MustMarshal(&data)
	}
	return channeltypesv2.Payload{
		SourcePort:      ibctransfertypes.PortID,
		DestinationPort: ibctransfertypes.PortID,
		Version:         ibctransfertypes.V1,
		Encoding:        encoding,
		Value:           value,
	}
}

// sendPacket wraps payloads in the v2 send message, signed by source.
func sendPacket(signer string, payloads ...channeltypesv2.Payload) *channeltypesv2.MsgSendPacket {
	return &channeltypesv2.MsgSendPacket{
		SourceClient:     "07-tendermint-0",
		TimeoutTimestamp: 1,
		Payloads:         payloads,
		Signer:           signer,
	}
}

// The execution surfaces D41 assigns to the Wasm dispatcher — IBC sends,
// execute funds, and instantiate funds — are taxed by this same calculator, and
// the cap applies to each independently as it does to a Bank send.
func (s *KeeperTestSuite) TestComputeTaxCoversTransferAndContractFunds() {
	source := authtypes.NewModuleAddress("tax-source").String()
	contract := authtypes.NewModuleAddress("tax-contract").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(1_000))

	testCases := []struct {
		name     string
		msg      sdk.Msg
		expected sdk.Coins
	}{
		{
			name:     "IBC transfer taxes the outbound token",
			msg:      &ibctransfertypes.MsgTransfer{Sender: source, Token: sdk.NewInt64Coin(chain.USDBaseDenom, 500)},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)),
		},
		{
			name: "IBC v2 send packet taxes the JSON-encoded outbound token",
			msg: sendPacket(source, transferPayload(
				ibctransfertypes.EncodingJSON, chain.USDBaseDenom, "500", source)),
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)),
		},
		{
			name: "IBC v2 send packet taxes the protobuf-encoded outbound token",
			msg: sendPacket(source, transferPayload(
				ibctransfertypes.EncodingProtobuf, chain.USDBaseDenom, "500", source)),
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)),
		},
		{
			name: "the cap binds a v2 send packet as it binds a transfer",
			msg: sendPacket(source, transferPayload(
				ibctransfertypes.EncodingJSON, chain.USDBaseDenom, "100000", source)),
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
		},
		{
			// The source port selects the app that runs, so a payload bound
			// for one that moves no coins is never decoded — these bytes are
			// not ICS-20 and would fail if it were.
			name: "a non-transfer v2 payload is untaxed and never decoded",
			msg: sendPacket(source, channeltypesv2.Payload{
				SourcePort:      gmptypes.PortID,
				DestinationPort: gmptypes.PortID,
				Version:         "gmp-1",
				Encoding:        ibctransfertypes.EncodingProtobuf,
				Value:           []byte("not ICS-20"),
			}),
			expected: sdk.NewCoins(),
		},
		{
			// A voucher leaving carries its trace, so ToCoin hashes it to an
			// ibc/ denomination that holds no cap: outside the tax base, as
			// the same asset is under MsgTransfer.
			name: "a traced foreign voucher is outside the tax base",
			msg: sendPacket(source, transferPayload(
				ibctransfertypes.EncodingJSON, "transfer/channel-0/uatom", "500", source)),
			expected: sdk.NewCoins(),
		},
		{
			name:     "execute funds are taxed",
			msg:      &wasmtypes.MsgExecuteContract{Sender: source, Contract: contract, Funds: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 300))},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 30)),
		},
		{
			name:     "instantiate funds are taxed",
			msg:      &wasmtypes.MsgInstantiateContract{Sender: source, Funds: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 200))},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)),
		},
		{
			name:     "instantiate2 funds are taxed",
			msg:      &wasmtypes.MsgInstantiateContract2{Sender: source, Funds: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 200))},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)),
		},
		{
			name:     "store-and-instantiate funds are taxed",
			msg:      &wasmtypes.MsgStoreAndInstantiateContract{Authority: source, Funds: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 200))},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)),
		},
		{
			name:     "fundless execution is untaxed",
			msg:      &wasmtypes.MsgExecuteContract{Sender: source, Contract: contract},
			expected: sdk.NewCoins(),
		},
		{
			name:     "the cap binds each execution input alone",
			msg:      &ibctransfertypes.MsgTransfer{Sender: source, Token: sdk.NewInt64Coin(chain.USDBaseDenom, 100_000)},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{tc.msg})
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, tax)
		})
	}
}

// Funding a vesting account moves the coins at creation — only release is
// scheduled — so every vesting shape is taxed like the send it is. Terra
// Classic left these untaxed and they became the standard dodge around its
// transfer tax.
func (s *KeeperTestSuite) TestComputeTaxCoversVestingAccountFunding() {
	source := authtypes.NewModuleAddress("tax-source").String()
	recipient := authtypes.NewModuleAddress("tax-recipient").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(100))
	s.setDerivedTaxCap(chain.KRWBaseDenom, math.NewInt(100))

	nested, err := codectypes.NewAnyWithValue(&vestingtypes.MsgCreateVestingAccount{
		FromAddress: source,
		ToAddress:   recipient,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 500)),
	})
	s.Require().NoError(err)

	testCases := []struct {
		name     string
		msg      sdk.Msg
		expected sdk.Coins
	}{
		{
			name: "continuous vesting amount is taxed",
			msg: &vestingtypes.MsgCreateVestingAccount{
				FromAddress: source,
				ToAddress:   recipient,
				Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 500)),
			},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)),
		},
		{
			name: "permanent locked amount is taxed",
			msg: &vestingtypes.MsgCreatePermanentLockedAccount{
				FromAddress: source,
				ToAddress:   recipient,
				Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 500)),
			},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)),
		},
		{
			// 10% of the 1400 total clamps once at the 100 cap; per-period
			// inputs would have paid 70 twice through two caps.
			name: "periodic schedule sums to one capped input",
			msg: &vestingtypes.MsgCreatePeriodicVestingAccount{
				FromAddress: source,
				ToAddress:   recipient,
				VestingPeriods: []vestingtypes.Period{
					{Length: 60, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
					{Length: 60, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 700))},
				},
			},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
		},
		{
			name: "periodic schedule sums each denomination",
			msg: &vestingtypes.MsgCreatePeriodicVestingAccount{
				FromAddress: source,
				ToAddress:   recipient,
				VestingPeriods: []vestingtypes.Period{
					{Length: 60, Amount: sdk.NewCoins(
						sdk.NewInt64Coin(chain.KRWBaseDenom, 200),
						sdk.NewInt64Coin(chain.USDBaseDenom, 100),
					)},
					{Length: 60, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))},
				},
			},
			expected: sdk.NewCoins(
				sdk.NewInt64Coin(chain.KRWBaseDenom, 20),
				sdk.NewInt64Coin(chain.USDBaseDenom, 20),
			),
		},
		{
			name:     "vesting nested in authz is still taxed",
			msg:      &authz.MsgExec{Msgs: []*codectypes.Any{nested}},
			expected: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{tc.msg})
			s.Require().NoError(err)
			s.Require().Equal(tc.expected, tax)
		})
	}
}

func (s *KeeperTestSuite) TestComputeTaxRejectsMalformedVestingMessages() {
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))

	maxInt := math.NewIntFromBigInt(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	var typedNil *vestingtypes.MsgCreatePeriodicVestingAccount

	testCases := []struct {
		name   string
		msg    sdk.Msg
		errStr string
	}{
		{
			name:   "typed nil periodic message",
			msg:    typedNil,
			errStr: "nil periodic vesting account message",
		},
		{
			name: "invalid period coins",
			msg: &vestingtypes.MsgCreatePeriodicVestingAccount{
				VestingPeriods: []vestingtypes.Period{
					{Length: 60, Amount: sdk.Coins{sdk.Coin{Denom: "", Amount: math.OneInt()}}},
				},
			},
			errStr: "invalid taxable coins",
		},
		{
			name: "period sum overflow",
			msg: &vestingtypes.MsgCreatePeriodicVestingAccount{
				VestingPeriods: []vestingtypes.Period{
					{Length: 60, Amount: sdk.Coins{sdk.Coin{Denom: chain.USDBaseDenom, Amount: maxInt}}},
					{Length: 60, Amount: sdk.Coins{sdk.Coin{Denom: chain.USDBaseDenom, Amount: maxInt}}},
				},
			},
			errStr: "summing vesting periods",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			_, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{tc.msg})
			s.Require().ErrorIs(err, types.ErrInvalidTaxMessage)
			s.Require().ErrorContains(err, tc.errStr)
		})
	}
}

// Nesting is the path that skips any top-level check, so the v2 send is
// priced there too.
func (s *KeeperTestSuite) TestComputeTaxTaxesSendPacketNestedInAuthz() {
	source := authtypes.NewModuleAddress("tax-source").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(1_000))

	nested, err := codectypes.NewAnyWithValue(sendPacket(source, transferPayload(
		ibctransfertypes.EncodingJSON, chain.USDBaseDenom, "500", source)))
	s.Require().NoError(err)

	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{nested}}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 50)), tax)
}

// A transfer payload this cannot decode fails the transaction rather than
// passing untaxed: the decode is the module's own, so anything refused here
// would be refused at the escrow, and skipping would reopen the seam the case
// closes.
func (s *KeeperTestSuite) TestComputeTaxRejectsMalformedSendPacket() {
	source := authtypes.NewModuleAddress("tax-source").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))

	var typedNil *channeltypesv2.MsgSendPacket

	malformed := func(mutate func(*channeltypesv2.Payload)) sdk.Msg {
		payload := transferPayload(ibctransfertypes.EncodingJSON, chain.USDBaseDenom, "500", source)
		mutate(&payload)
		return sendPacket(source, payload)
	}

	testCases := []struct {
		name   string
		msg    sdk.Msg
		errStr string
	}{
		{
			name:   "typed nil send packet",
			msg:    typedNil,
			errStr: "nil IBC v2 send packet message",
		},
		{
			name:   "undecodable payload value",
			msg:    malformed(func(p *channeltypesv2.Payload) { p.Value = []byte("{") }),
			errStr: "cannot unmarshal",
		},
		{
			name:   "unsupported ICS-20 version",
			msg:    malformed(func(p *channeltypesv2.Payload) { p.Version = "ics20-2" }),
			errStr: "ics20-2",
		},
		{
			name:   "unsupported encoding",
			msg:    malformed(func(p *channeltypesv2.Payload) { p.Encoding = "application/x-nonsense" }),
			errStr: "invalid encoding provided",
		},
		{
			name: "non-positive amount",
			msg: sendPacket(source, transferPayload(
				ibctransfertypes.EncodingJSON, chain.USDBaseDenom, "0", source)),
			errStr: "amount must be strictly positive",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			_, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{tc.msg})
			s.Require().ErrorIs(err, types.ErrInvalidTaxMessage)
			s.Require().ErrorContains(err, tc.errStr)
		})
	}
}

func (s *KeeperTestSuite) TestComputeTaxAppliesCapPerMultiSendInput() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	recipientA := authtypes.NewModuleAddress("tax-recipient-a").String()
	recipientB := authtypes.NewModuleAddress("tax-recipient-b").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(50))

	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgMultiSend{
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
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(10))
	s.setAssets(chain.XDRBaseDenom)

	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
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
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setAssets(chain.USDBaseDenom)

	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100))},
	})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
}

// TestComputeTaxNeverTaxesNoah pins GetTaxCap's exclusion where it matters:
// NOAH's factor sits in the same table as the members' for fee pricing, and
// the numeraire still moves untaxed beside a taxed member.
func (s *KeeperTestSuite) TestComputeTaxNeverTaxesNoah() {
	source := authtypes.NewModuleAddress("tax-source").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, types.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.NoahBaseDenom, types.ConversionFactor{
		Denom:  chain.NoahBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(
			sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000),
			sdk.NewInt64Coin(chain.USDBaseDenom, 1_000),
		)},
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), tax)
}

func (s *KeeperTestSuite) TestComputeTaxTreatsZeroReferenceAsUncapped() {
	source := authtypes.NewModuleAddress("tax-source").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	// The suite baseline's zero reference derives the uncapped sentinel; the
	// held factor is what keeps the denomination taxed at all.
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, types.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000))},
	})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), tax)
}

func (s *KeeperTestSuite) TestComputeTaxRejectsMalformedMessagesWhenDisabled() {
	var send *banktypes.MsgSend
	_, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{send})
	s.Require().ErrorIs(err, types.ErrInvalidTaxMessage)
}

func (s *KeeperTestSuite) TestComputeTaxReturnsZeroWithoutOracleLookupWhenDisabled() {
	source := authtypes.NewModuleAddress("tax-source").String()
	tax, base, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		FromAddress: source,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
	// The base is reported at a zero rate too: what moves does not depend
	// on whether it is taxed.
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)), base)
}

func (s *KeeperTestSuite) TestComputeTaxRejectsMalformedNestedMessage() {
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))

	badSwapSend := &markettypes.MsgSwapSend{
		OfferCoin: sdk.Coin{Denom: "", Amount: math.OneInt()},
	}
	badAny, err := codectypes.NewAnyWithValue(badSwapSend)
	s.Require().NoError(err)

	_, _, err = s.keeper.ComputeTax(s.ctx, []sdk.Msg{&authz.MsgExec{Msgs: []*codectypes.Any{badAny}}})
	s.Require().ErrorContains(err, "invalid taxable coins")
}

func (s *KeeperTestSuite) TestComputeTaxRejectsAuthzAnyWithoutCachedMessage() {
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))

	_, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&authz.MsgExec{
		Msgs: []*codectypes.Any{{TypeUrl: "/ark.market.v1.MsgSwapSend"}},
	}})
	s.Require().ErrorContains(err, "not a sdk.MsgRequest")
}

func (s *KeeperTestSuite) TestComputeTaxRejectsTypedNilMessages() {
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))

	var send *banktypes.MsgSend
	_, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{send})
	s.Require().ErrorContains(err, "nil bank send message")
}

func (s *KeeperTestSuite) TestComputeTaxRecursesThroughAuthzAndFiltersDenoms() {
	sourceA := authtypes.NewModuleAddress("tax-source-a").String()
	sourceB := authtypes.NewModuleAddress("tax-source-b").String()
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
	s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(1_000))
	s.setDerivedTaxCap(chain.KRWBaseDenom, math.NewInt(1_000))

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
			tax, _, err := s.keeper.ComputeTax(s.ctx, test.msgs())
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
	maxParams := types.DefaultParams()
	maxParams.TransferTaxRate = math.LegacyOneDec()
	maxParams.ReferenceTaxCap = maxInt
	s.Require().NoError(s.keeper.Params.Set(s.ctx, maxParams))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, chain.USDBaseDenom, types.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))
	source := authtypes.NewModuleAddress("tax-source").String()
	msg := func() sdk.Msg {
		return &banktypes.MsgSend{
			FromAddress: source,
			Amount:      sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, maxInt)),
		}
	}

	_, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{msg(), msg()})
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
			s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))
			s.setDerivedTaxCap(chain.USDBaseDenom, math.NewInt(1_000))
			s.seedAsset(chain.USDBaseDenom, status)

			tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
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
	s.setTransferTaxRate(math.LegacyMustNewDecFromStr("0.1"))

	tax, _, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{
		&banktypes.MsgSend{FromAddress: source, Amount: sdk.NewCoins(sdk.NewInt64Coin("aatom", 100))},
	})
	s.Require().NoError(err)
	s.Require().True(tax.IsZero())
}
