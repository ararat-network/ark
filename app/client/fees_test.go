package client

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	sdkmath "cosmossdk.io/math"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	markettypes "github.com/ararat-network/ark/x/market/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// fakeChain serves the queries the helper makes, over an in-process gRPC
// connection: the tax for whatever messages it is sent, the gas-price sheet,
// and one address's spendable balances.
type fakeChain struct {
	treasurytypes.UnimplementedQueryServer
	tax       sdk.Coins
	sheet     treasurytypes.QueryGasPricesResponse
	spendable sdk.Coins

	taxed [][]string // type URLs of the messages each tax query carried
}

func (c *fakeChain) ComputeTax(_ context.Context, req *treasurytypes.QueryComputeTaxRequest) (*treasurytypes.QueryComputeTaxResponse, error) {
	urls := make([]string, len(req.Messages))
	for i, msg := range req.Messages {
		urls[i] = msg.TypeUrl
	}
	c.taxed = append(c.taxed, urls)
	return &treasurytypes.QueryComputeTaxResponse{Tax: c.tax}, nil
}

func (c *fakeChain) GasPrices(context.Context, *treasurytypes.QueryGasPricesRequest) (*treasurytypes.QueryGasPricesResponse, error) {
	sheet := c.sheet
	return &sheet, nil
}

// bankBalances is the bank half of the fake, its own server because the two
// query services share a Params method.
type bankBalances struct {
	banktypes.UnimplementedQueryServer
	spendable sdk.Coins
}

func (b *bankBalances) SpendableBalances(context.Context, *banktypes.QuerySpendableBalancesRequest) (*banktypes.QuerySpendableBalancesResponse, error) {
	return &banktypes.QuerySpendableBalancesResponse{Balances: b.spendable}, nil
}

// connect serves the fake and returns a client context dialled to it, with
// the gogo codec both ends need.
func connect(t *testing.T, chain *fakeChain, from sdk.AccAddress) sdkclient.Context {
	t.Helper()
	grpcCodec := codec.NewProtoCodec(codectypes.NewInterfaceRegistry()).GRPCCodec()
	server := grpc.NewServer(grpc.ForceServerCodec(grpcCodec))
	treasurytypes.RegisterQueryServer(server, chain)
	banktypes.RegisterQueryServer(server, &bankBalances{spendable: chain.spendable})
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()

	conn, err := grpc.NewClient("passthrough:///chain",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcCodec)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
	})
	return sdkclient.Context{GRPCClient: conn, FromAddress: from}
}

// TestPriceFee pins the completion's pricing: the chain's tax rides beside
// the gas fee the caller priced, or one from the sheet — the reference row
// without a payer, else the first denomination in the recorded order whose
// spendable balance covers the declaration — every stable leg lifted, NOAH
// exact. Offline, only a NOAH transfer prices.
func TestPriceFee(t *testing.T) {
	sender := sdk.AccAddress("sender")
	msg := banktypes.NewMsgSend(
		sender,
		sdk.AccAddress("recipient"),
		sdk.NewCoins(sdk.NewInt64Coin("axdr", 100)),
	)
	noahMsg := banktypes.NewMsgSend(
		sender,
		sdk.AccAddress("recipient"),
		sdk.NewCoins(sdk.NewInt64Coin("anoah", 100)),
	)
	sheet := treasurytypes.QueryGasPricesResponse{
		GasPrices: []treasurytypes.GasPrice{
			{Denom: "anoah", GasPrice: sdkmath.LegacyMustNewDecFromStr("0.025"), DerivedHeight: 6},
		},
		ReferenceGasPrice: treasurytypes.GasPrice{Denom: "axdr", GasPrice: sdkmath.LegacyMustNewDecFromStr("0.1")},
	}
	oneTax := sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))
	noahGasFee := sdk.NewCoins(sdk.NewInt64Coin("anoah", 25))
	// 200k gas at these prices: 20,000axdr or 5,000anoah. Lifted by the
	// headroom, the axdr leg with its one of tax declares 22,002; NOAH is
	// exact.
	const gas = 200_000

	t.Run("a priced gas fee takes the chain's tax, lifted", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax}
		got, err := priceFee(connect(t, chain, nil), 100, noahGasFee, []sdk.Msg{msg})

		require.NoError(t, err)
		require.Equal(t, "25anoah", got.Gas.String())
		require.Equal(t, "1axdr", got.Tax.String())
		// The tax leg is lifted; NOAH gas is exact.
		require.Equal(t, "25anoah,2axdr", got.Fee.String())
		// The chain priced the very message being sent.
		require.Equal(t, [][]string{{"/cosmos.bank.v1beta1.MsgSend"}}, chain.taxed)
	})

	t.Run("an untaxed message adds nothing", func(t *testing.T) {
		got, err := priceFee(connect(t, &fakeChain{}, nil), 100, noahGasFee, []sdk.Msg{noahMsg})

		require.NoError(t, err)
		require.True(t, got.Tax.IsZero())
		require.Equal(t, "25anoah", got.Fee.String())
	})

	t.Run("zero gas prices nothing", func(t *testing.T) {
		got, err := priceFee(sdkclient.Context{Offline: true}, 0, nil, []sdk.Msg{msg})

		require.NoError(t, err)
		require.Equal(t, FeeBreakdown{}, got)
	})

	t.Run("a priced gas fee works offline for a NOAH transfer", func(t *testing.T) {
		gasFee := sdk.NewCoins(sdk.NewInt64Coin("anoah", 100))
		got, err := priceFee(sdkclient.Context{Offline: true}, 200, gasFee, []sdk.Msg{noahMsg})

		require.NoError(t, err)
		require.Equal(t, "100anoah", got.Fee.String())
	})

	t.Run("offline refuses a transfer that may owe tax", func(t *testing.T) {
		gasFee := sdk.NewCoins(sdk.NewInt64Coin("anoah", 100))
		_, err := priceFee(sdkclient.Context{Offline: true}, 200, gasFee, []sdk.Msg{msg})

		require.ErrorContains(t, err, "transferring 100axdr may owe a transfer tax")
		require.ErrorContains(t, err, "provide --fees")
	})

	t.Run("the sheet prices gas in a denomination covering gas, transfer, and tax", func(t *testing.T) {
		chain := &fakeChain{
			tax:       oneTax,
			sheet:     sheet,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 22_102), sdk.NewInt64Coin("anoah", 5_000)),
		}
		got, err := priceFee(connect(t, chain, sender), gas, nil, []sdk.Msg{msg})

		require.NoError(t, err)
		require.Equal(t, "22002axdr", got.Fee.String())
	})

	t.Run("the tax counts against the transferred denomination's balance", func(t *testing.T) {
		chain := &fakeChain{
			tax:       oneTax,
			sheet:     sheet,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 22_101), sdk.NewInt64Coin("anoah", 5_000)),
		}
		got, err := priceFee(connect(t, chain, sender), gas, nil, []sdk.Msg{msg})

		require.NoError(t, err)
		// One short of the lifted gas and tax plus the transfer in axdr, so
		// gas rides NOAH and the tax still rides axdr, lifted.
		require.Equal(t, "5000anoah,2axdr", got.Fee.String())
	})

	t.Run("without a payer the reference row prices gas beside the tax", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax, sheet: sheet}
		got, err := priceFee(connect(t, chain, nil), gas, nil, []sdk.Msg{msg})

		require.NoError(t, err)
		require.Equal(t, "22002axdr", got.Fee.String())
	})

	t.Run("nothing covering names the tax", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax, sheet: sheet}
		_, err := priceFee(connect(t, chain, sender), gas, nil, []sdk.Msg{msg})

		require.ErrorContains(t, err,
			"no spendable balance covers the gas fee (20000axdr at the posted prices) beside the transfer tax 1axdr")
	})
}

// TestPickFeeDenom pins the recorded fee-denomination walk: transferred
// denominations first, NOAH, the reference, then the rest of the sheet —
// each candidate needing a price row and a spendable balance covering the
// lifted gas fee and tax plus any same-denomination transfer. The reference
// rides beside the sheet as its own row, never a list entry.
func TestPickFeeDenom(t *testing.T) {
	reference := treasurytypes.GasPrice{Denom: "axdr", GasPrice: sdkmath.LegacyMustNewDecFromStr("0.1")}
	sheet := []treasurytypes.GasPrice{
		{Denom: "anoah", GasPrice: sdkmath.LegacyMustNewDecFromStr("0.025"), DerivedHeight: 6},
		{Denom: "ausd", GasPrice: sdkmath.LegacyMustNewDecFromStr("0.2"), DerivedHeight: 5},
	}
	gasLimit := sdkmath.LegacyNewDec(200_000)
	noTax := sdk.NewCoins()
	// Fees at these prices: 20,000axdr, 40,000ausd, 5,000anoah; lifted,
	// 22,000axdr and 44,000ausd, NOAH exact.

	t.Run("transferred denom leads when it covers transfer plus fee", func(t *testing.T) {
		fee, ok := pickFeeDenom(
			sheet,
			reference,
			sdk.NewCoins(sdk.NewInt64Coin("ausd", 44_100), sdk.NewInt64Coin("anoah", 1_000_000)),
			sdk.NewCoins(sdk.NewInt64Coin("ausd", 100)),
			noTax,
			gasLimit,
		)
		require.True(t, ok)
		require.Equal(t, sdk.NewInt64Coin("ausd", 40_000), fee)
	})

	t.Run("the tax counts against the same balance", func(t *testing.T) {
		// Lifted gas and tax are 44,006; with the transfer, one short.
		fee, ok := pickFeeDenom(
			sheet,
			reference,
			sdk.NewCoins(sdk.NewInt64Coin("ausd", 44_105), sdk.NewInt64Coin("anoah", 1_000_000)),
			sdk.NewCoins(sdk.NewInt64Coin("ausd", 100)),
			sdk.NewCoins(sdk.NewInt64Coin("ausd", 5)),
			gasLimit,
		)
		require.True(t, ok)
		require.Equal(t, sdk.NewInt64Coin("anoah", 5_000), fee)
	})

	t.Run("a transferred reference amount prices through the first tier", func(t *testing.T) {
		fee, ok := pickFeeDenom(
			sheet,
			reference,
			sdk.NewCoins(sdk.NewInt64Coin("axdr", 22_100), sdk.NewInt64Coin("anoah", 1_000_000)),
			sdk.NewCoins(sdk.NewInt64Coin("axdr", 100)),
			noTax,
			gasLimit,
		)
		require.True(t, ok)
		require.Equal(t, sdk.NewInt64Coin("axdr", 20_000), fee)
	})

	t.Run("a transfer the balance cannot also fee falls through to noah", func(t *testing.T) {
		fee, ok := pickFeeDenom(
			sheet,
			reference,
			sdk.NewCoins(sdk.NewInt64Coin("ausd", 40_050), sdk.NewInt64Coin("anoah", 5_000)),
			sdk.NewCoins(sdk.NewInt64Coin("ausd", 100)),
			noTax,
			gasLimit,
		)
		require.True(t, ok)
		require.Equal(t, sdk.NewInt64Coin("anoah", 5_000), fee)
	})

	t.Run("noah precedes the reference for a transferless transaction", func(t *testing.T) {
		fee, ok := pickFeeDenom(
			sheet,
			reference,
			sdk.NewCoins(sdk.NewInt64Coin("anoah", 5_000), sdk.NewInt64Coin("axdr", 1_000_000)),
			sdk.NewCoins(),
			noTax,
			gasLimit,
		)
		require.True(t, ok)
		require.Equal(t, sdk.NewInt64Coin("anoah", 5_000), fee)
	})

	t.Run("an unpriced transferred denom is skipped", func(t *testing.T) {
		fee, ok := pickFeeDenom(
			sheet,
			reference,
			sdk.NewCoins(sdk.NewInt64Coin("akrw", 1_000_000), sdk.NewInt64Coin("axdr", 22_000)),
			sdk.NewCoins(sdk.NewInt64Coin("akrw", 100)),
			noTax,
			gasLimit,
		)
		require.True(t, ok)
		require.Equal(t, sdk.NewInt64Coin("axdr", 20_000), fee)
	})

	t.Run("nothing sufficient reports failure", func(t *testing.T) {
		_, ok := pickFeeDenom(sheet, reference, sdk.NewCoins(sdk.NewInt64Coin("axdr", 19_999)), sdk.NewCoins(), noTax, gasLimit)
		require.False(t, ok)
	})
}

// TestTransferredCoins pins the best-effort message scan the walk leads with.
func TestTransferredCoins(t *testing.T) {
	coins := transferredCoins([]sdk.Msg{
		&banktypes.MsgSend{Amount: sdk.NewCoins(sdk.NewInt64Coin("ausd", 100))},
		&markettypes.MsgSwap{OfferCoin: sdk.NewInt64Coin("anoah", 7)},
	})
	require.Equal(t, sdk.NewCoins(
		sdk.NewInt64Coin("anoah", 7),
		sdk.NewInt64Coin("ausd", 100),
	), coins)
}

// TestWithTip pins the tip: NOAH alone, added to whatever fee stands, a zero
// tip a no-op.
func TestWithTip(t *testing.T) {
	txf := clienttx.Factory{}.WithFees("22002axdr")

	got, err := WithTip(txf, sdk.NewInt64Coin("anoah", 1_000))
	require.NoError(t, err)
	require.Equal(t, "1000anoah,22002axdr", got.Fees().String())

	got, err = WithTip(txf, sdk.NewInt64Coin("anoah", 0))
	require.NoError(t, err)
	require.Equal(t, txf.Fees(), got.Fees())

	_, err = WithTip(txf, sdk.NewInt64Coin("axdr", 1))
	require.ErrorContains(t, err, "tips are paid in anoah")
}
