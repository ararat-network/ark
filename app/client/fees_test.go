package client

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/math"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// fakeChain serves the queries the helper makes, over an in-process gRPC
// connection: the tax and its base for whatever messages it is sent, the
// gas-price sheet, and one address's spendable balances.
type fakeChain struct {
	treasurytypes.UnimplementedQueryServer
	txtypes.UnimplementedServiceServer
	tax       sdk.Coins
	base      sdk.Coins
	sheet     treasurytypes.QueryGasPricesResponse
	spendable sdk.Coins

	gasUsed uint64 // what a simulation reports

	taxed    [][]string // type URLs of the messages each tax query carried
	sheets   int        // gas-price sheet queries
	balances []string   // addresses the spendable balance queries named
}

// Simulate is the SDK's own service, which --gas auto goes through before
// the transaction it prices is built.
func (c *fakeChain) Simulate(context.Context, *txtypes.SimulateRequest) (*txtypes.SimulateResponse, error) {
	return &txtypes.SimulateResponse{GasInfo: &sdk.GasInfo{GasUsed: c.gasUsed}}, nil
}

func (c *fakeChain) ComputeTax(_ context.Context, req *treasurytypes.QueryComputeTaxRequest) (*treasurytypes.QueryComputeTaxResponse, error) {
	urls := make([]string, len(req.Messages))
	for i, msg := range req.Messages {
		urls[i] = msg.TypeUrl
	}
	c.taxed = append(c.taxed, urls)
	return &treasurytypes.QueryComputeTaxResponse{Tax: c.tax, TaxBase: c.base}, nil
}

func (c *fakeChain) GasPrices(context.Context, *treasurytypes.QueryGasPricesRequest) (*treasurytypes.QueryGasPricesResponse, error) {
	c.sheets++
	sheet := c.sheet
	return &sheet, nil
}

// bankBalances is the bank half of the fake, its own server because the two
// query services share a Params method.
type bankBalances struct {
	banktypes.UnimplementedQueryServer
	chain *fakeChain
}

func (b *bankBalances) SpendableBalances(_ context.Context, req *banktypes.QuerySpendableBalancesRequest) (*banktypes.QuerySpendableBalancesResponse, error) {
	b.chain.balances = append(b.chain.balances, req.Address)
	return &banktypes.QuerySpendableBalancesResponse{Balances: b.chain.spendable}, nil
}

// connect serves the fake and returns a client context dialled to it, with
// the gogo codec both ends need.
func connect(t *testing.T, chain *fakeChain) sdkclient.Context {
	t.Helper()
	grpcCodec := codec.NewProtoCodec(codectypes.NewInterfaceRegistry()).GRPCCodec()
	server := grpc.NewServer(grpc.ForceServerCodec(grpcCodec))
	treasurytypes.RegisterQueryServer(server, chain)
	txtypes.RegisterServiceServer(server, chain)
	banktypes.RegisterQueryServer(server, &bankBalances{chain: chain})
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
	return sdkclient.Context{GRPCClient: conn}
}

// TestPricing pins the pricing's two halves: what the chain is asked about
// a transaction, and the declaration priced from it against its gas — the
// chain's tax beside the gas fee the command priced, or one from the sheet:
// the reference row without a payer, else the first denomination in the
// recorded order whose spendable balance covers the declaration, every stable
// leg lifted and NOAH exact. Offline there is no chain to ask.
func TestPricing(t *testing.T) {
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
			{Denom: "anoah", GasPrice: math.LegacyMustNewDecFromStr("0.025"), DerivedHeight: 6},
		},
		ReferenceGasPrice: treasurytypes.GasPrice{Denom: "axdr", GasPrice: math.LegacyMustNewDecFromStr("0.1")},
	}
	oneTax := sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))
	transferred := sdk.NewCoins(sdk.NewInt64Coin("axdr", 100))
	noahGasFee := sdk.NewCoins(sdk.NewInt64Coin("anoah", 25))
	// 200k gas at these prices: 20,000axdr or 5,000anoah. Lifted by the
	// headroom, the axdr leg with its one of tax declares 22,002; NOAH is
	// exact.
	const gas = 200_000

	// price runs the halves as the builder runs them: the chain asked about
	// the messages, the fee settled against a gas figure. A command that
	// priced its own gas leaves the sheet unasked.
	price := func(
		t *testing.T,
		clientCtx sdkclient.Context,
		payer sdk.AccAddress,
		sends bool,
		gas uint64,
		gasFee sdk.Coins,
		msgs ...sdk.Msg,
	) (FeeBreakdown, error) {
		t.Helper()
		quote, err := quoteFee(clientCtx, msgs, payer, sends, gasFee.IsZero())
		if err != nil {
			return FeeBreakdown{}, err
		}
		return quote.price(gas, gasFee, nil)
	}

	t.Run("a priced gas fee takes the chain's tax, lifted", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax}
		got, err := price(t, connect(t, chain), nil, false, 100, noahGasFee, msg)

		require.NoError(t, err)
		require.Equal(t, "25anoah", got.Gas.String())
		require.Equal(t, "1axdr", got.Tax.String())
		// The tax leg is lifted; NOAH gas is exact.
		require.Equal(t, "25anoah,2axdr", got.Fee.String())
		// The chain priced the very message being sent, and was asked for
		// nothing it could not use.
		require.Equal(t, [][]string{{"/cosmos.bank.v1beta1.MsgSend"}}, chain.taxed)
		require.Zero(t, chain.sheets)
		require.Empty(t, chain.balances)
	})

	t.Run("an untaxed message adds nothing", func(t *testing.T) {
		got, err := price(t, connect(t, &fakeChain{}), nil, false, 100, noahGasFee, noahMsg)

		require.NoError(t, err)
		require.True(t, got.Tax.IsZero())
		require.Equal(t, "25anoah", got.Fee.String())
	})

	t.Run("zero gas prices nothing", func(t *testing.T) {
		got, err := price(t, connect(t, &fakeChain{}), nil, false, 0, nil, noahMsg)

		require.NoError(t, err)
		require.Equal(t, FeeBreakdown{}, got)
	})

	t.Run("offline refuses without an explicit fee", func(t *testing.T) {
		gasFee := sdk.NewCoins(sdk.NewInt64Coin("anoah", 100))
		_, err := price(t, sdkclient.Context{Offline: true}, nil, false, 200, gasFee, noahMsg)

		require.ErrorContains(t, err, "offline, the transfer tax cannot be priced; provide --fees")
	})

	t.Run("the sheet prices gas in a denomination covering gas, transfer, and tax", func(t *testing.T) {
		chain := &fakeChain{
			tax:       oneTax,
			base:      transferred,
			sheet:     sheet,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 22_102), sdk.NewInt64Coin("anoah", 5_000)),
		}
		got, err := price(t, connect(t, chain), sender, true, gas, nil, msg)

		require.NoError(t, err)
		require.Equal(t, "22002axdr", got.Fee.String())
	})

	t.Run("a fee drawn on another account is not netted", func(t *testing.T) {
		// A granter's balance covers the lifted gas and tax exactly; the
		// transfer leaves the signer, not the granter, and does not count.
		chain := &fakeChain{
			tax:       oneTax,
			base:      transferred,
			sheet:     sheet,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 22_002)),
		}
		got, err := price(t, connect(t, chain), sender, false, gas, nil, msg)

		require.NoError(t, err)
		require.Equal(t, "22002axdr", got.Fee.String())
	})

	t.Run("the tax counts against the transferred denomination's balance", func(t *testing.T) {
		chain := &fakeChain{
			tax:       oneTax,
			base:      transferred,
			sheet:     sheet,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 22_101), sdk.NewInt64Coin("anoah", 5_000)),
		}
		got, err := price(t, connect(t, chain), sender, true, gas, nil, msg)

		require.NoError(t, err)
		// One short of the lifted gas and tax plus the transfer in axdr, so
		// gas rides NOAH and the tax still rides axdr, lifted.
		require.Equal(t, "5000anoah,2axdr", got.Fee.String())
	})

	t.Run("without a payer the reference row prices gas beside the tax", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax, base: transferred, sheet: sheet}
		got, err := price(t, connect(t, chain), nil, false, gas, nil, msg)

		require.NoError(t, err)
		require.Equal(t, "22002axdr", got.Fee.String())
		require.Empty(t, chain.balances)
	})

	t.Run("nothing covering names the tax", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax, base: transferred, sheet: sheet}
		_, err := price(t, connect(t, chain), sender, true, gas, nil, msg)

		require.ErrorContains(t, err,
			"no spendable balance covers the gas fee (20000axdr at the posted prices) beside the transfer tax 1axdr")
	})
}

// TestPickFeeDenom pins the recorded fee-denomination walk: the denominations
// the transaction moves first, NOAH, the reference, then the rest of the sheet —
// each candidate needing a price row and a spendable balance covering the
// lifted gas fee and tax plus whatever of the transfer leaves the same
// account. The reference rides beside the sheet as its own row, never a list
// entry.
func TestPickFeeDenom(t *testing.T) {
	reference := treasurytypes.GasPrice{Denom: "axdr", GasPrice: math.LegacyMustNewDecFromStr("0.1")}
	sheet := []treasurytypes.GasPrice{
		{Denom: "anoah", GasPrice: math.LegacyMustNewDecFromStr("0.025"), DerivedHeight: 6},
		{Denom: "ausd", GasPrice: math.LegacyMustNewDecFromStr("0.2"), DerivedHeight: 5},
	}
	gasLimit := math.LegacyNewDec(200_000)
	coin := sdk.NewInt64Coin
	noTax := sdk.NewCoins()
	// Fees at these prices: 20,000axdr, 40,000ausd, 5,000anoah; lifted,
	// 22,000axdr and 44,000ausd, NOAH exact.

	// own is a transaction whose fee account is the one transferring, so the
	// transfer is netted; other is one paid from elsewhere.
	own := func(spendable, transferred, tax sdk.Coins) feeQuote {
		return feeQuote{sheet: sheet, reference: reference, spendable: spendable, transferred: transferred, spent: transferred, tax: tax}
	}
	other := func(spendable, transferred, tax sdk.Coins) feeQuote {
		return feeQuote{sheet: sheet, reference: reference, spendable: spendable, transferred: transferred, tax: tax}
	}

	tests := []struct {
		name  string
		quote feeQuote
		fee   sdk.Coin
		ok    bool
	}{
		{
			name:  "transferred denom leads when it covers transfer plus fee",
			quote: own(sdk.NewCoins(coin("ausd", 44_100), coin("anoah", 1_000_000)), sdk.NewCoins(coin("ausd", 100)), noTax),
			fee:   coin("ausd", 40_000),
			ok:    true,
		},
		{
			// Lifted gas and tax are 44,006; with the transfer, one short.
			name:  "the tax counts against the same balance",
			quote: own(sdk.NewCoins(coin("ausd", 44_105), coin("anoah", 1_000_000)), sdk.NewCoins(coin("ausd", 100)), sdk.NewCoins(coin("ausd", 5))),
			fee:   coin("anoah", 5_000),
			ok:    true,
		},
		{
			name:  "a transferred reference amount prices through the first tier",
			quote: own(sdk.NewCoins(coin("axdr", 22_100), coin("anoah", 1_000_000)), sdk.NewCoins(coin("axdr", 100)), noTax),
			fee:   coin("axdr", 20_000),
			ok:    true,
		},
		{
			name:  "a transfer the balance cannot also fee falls through to noah",
			quote: own(sdk.NewCoins(coin("ausd", 40_050), coin("anoah", 5_000)), sdk.NewCoins(coin("ausd", 100)), noTax),
			fee:   coin("anoah", 5_000),
			ok:    true,
		},
		{
			// The lifted fee exactly; netting the transfer would fall through.
			name:  "a transfer from another account is not netted",
			quote: other(sdk.NewCoins(coin("ausd", 44_000), coin("anoah", 1_000_000)), sdk.NewCoins(coin("ausd", 100)), noTax),
			fee:   coin("ausd", 40_000),
			ok:    true,
		},
		{
			name:  "noah precedes the reference for a transferless transaction",
			quote: own(sdk.NewCoins(coin("anoah", 5_000), coin("axdr", 1_000_000)), sdk.NewCoins(), noTax),
			fee:   coin("anoah", 5_000),
			ok:    true,
		},
		{
			name:  "an unpriced transferred denom is skipped",
			quote: own(sdk.NewCoins(coin("akrw", 1_000_000), coin("axdr", 22_000)), sdk.NewCoins(coin("akrw", 100)), noTax),
			fee:   coin("axdr", 20_000),
			ok:    true,
		},
		{
			name:  "nothing sufficient reports failure",
			quote: own(sdk.NewCoins(coin("axdr", 19_999)), sdk.NewCoins(), noTax),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fee, ok := tc.quote.pickFeeDenom(gasLimit)
			require.Equal(t, tc.ok, ok)
			if ok {
				require.Equal(t, tc.fee, fee)
			}
		})
	}
}
