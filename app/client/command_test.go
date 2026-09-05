package client

import (
	"bytes"
	"context"
	"io"
	"slices"
	"testing"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"

	bankv1beta1 "cosmossdk.io/api/cosmos/bank/v1beta1"
	"cosmossdk.io/math"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// sdkRun is a module transaction command's RunE: it builds its messages and
// hands them to the SDK. That handover is the whole of what a transaction
// command does, and going through it rather than mimicking it is what pins
// the seam — the factory reaching TxConfig.NewTxBuilder — to the SDK arkd
// builds against.
type sdkRun struct {
	txConfig sdkclient.TxConfig
	msgs     []sdk.Msg

	runs int
}

func (r *sdkRun) runE(cmd *cobra.Command, _ []string) error {
	r.runs++
	clientCtx, err := sdkclient.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	return clienttx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), r.msgs...)
}

// pricingFixture is a wrapped command over the fake chain, its stdout and
// stderr captured. The stored context is the persistent one a command starts
// from: --from, --offline, and the rest arrive as flags, as they do for arkd.
func pricingFixture(t *testing.T, chain *fakeChain, msgs ...sdk.Msg) (*cobra.Command, *sdkRun, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	encoding := moduletestutil.MakeTestEncodingConfig()
	banktypes.RegisterInterfaces(encoding.InterfaceRegistry)
	run := &sdkRun{txConfig: encoding.TxConfig, msgs: msgs}
	keys := keyring.NewInMemory(encoding.Codec)
	_, _, err := keys.NewMnemonic(payerKey, keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{Use: "send", RunE: run.runE}
	flags.AddTxFlagsToCmd(cmd)
	PriceTransactions(cmd)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	clientCtx := connect(t, chain).
		WithTxConfig(encoding.TxConfig).
		WithCodec(encoding.Codec).
		WithInterfaceRegistry(encoding.InterfaceRegistry).
		WithAccountRetriever(sdkclient.MockAccountRetriever{}).
		WithKeyring(keys).
		WithOutput(&stdout)
	require.NoError(t, sdkclient.SetCmdClientContext(cmd, clientCtx))
	return cmd, run, &stdout, &stderr
}

// payerKey is the fixture's signer: a key rather than a bare address,
// because the SDK's own simulation asks the keyring for the payer's public
// key and an address --from has no name to ask under.
const payerKey = "payer"

// TestPriceTransactions pins the pricing end to end, through the SDK's own
// transaction path: the command runs once and the fee it never declared is in
// the transaction it printed, with the parts on stderr. An explicit fee is
// left as declared.
func TestPriceTransactions(t *testing.T) {
	sender := sdk.AccAddress("sender")
	transferred := sdk.NewCoins(sdk.NewInt64Coin("axdr", 100))
	msg := banktypes.NewMsgSend(sender, sdk.AccAddress("recipient"), transferred)
	sheet := treasurytypes.QueryGasPricesResponse{
		GasPrices:         []treasurytypes.GasPrice{{Denom: "anoah", GasPrice: math.LegacyMustNewDecFromStr("0.025")}},
		ReferenceGasPrice: treasurytypes.GasPrice{Denom: "axdr", GasPrice: math.LegacyMustNewDecFromStr("0.1")},
	}
	oneTax := sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))
	// 200k gas at 0.1axdr is 20,000; with the one of tax, lifted, 22,002.
	// The estimate is 150k: 15,000, with the tax lifted 16,502.

	// execute runs the command under --generate-only, which prints the
	// transaction the pricing built instead of signing it. Offline, a chain
	// ID is the one flag the SDK refuses beside it.
	executeMsg := func(t *testing.T, chain *fakeChain, built sdk.Msg, args ...string) (*sdkRun, string, string, error) {
		t.Helper()
		cmd, run, stdout, stderr := pricingFixture(t, chain, built)
		flagged := append([]string{"--from", payerKey, "--generate-only"}, args...)
		if !slices.Contains(args, "--offline") {
			flagged = append(flagged, "--chain-id", "ark-test")
		}
		cmd.SetArgs(flagged)
		err := cmd.Execute()
		return run, stdout.String(), stderr.String(), err
	}
	execute := func(t *testing.T, chain *fakeChain, args ...string) (*sdkRun, string, string, error) {
		t.Helper()
		return executeMsg(t, chain, msg, args...)
	}

	// declared is the fee and gas of the transaction the command printed.
	declared := func(t *testing.T, run *sdkRun, stdout string) sdk.FeeTx {
		t.Helper()
		unsigned, err := run.txConfig.TxJSONDecoder()([]byte(stdout))
		require.NoError(t, err)
		feeTx, ok := unsigned.(sdk.FeeTx)
		require.True(t, ok)
		return feeTx
	}

	t.Run("an explicit fee is asked nothing and left as declared", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax}
		run, stdout, stderr, err := execute(t, chain, "--fees", "5axdr", "--gas", "100")
		require.NoError(t, err)
		require.Equal(t, 1, run.runs)
		require.Equal(t, "5axdr", declared(t, run, stdout).GetFee().String())
		require.Empty(t, stderr)
		require.Empty(t, chain.taxed)
	})

	t.Run("an explicit fee takes the tip", func(t *testing.T) {
		run, stdout, _, err := execute(t, &fakeChain{tax: oneTax},
			"--fees", "5axdr", "--gas", "100", "--tip", "7anoah")
		require.NoError(t, err)
		require.Equal(t, "7anoah,5axdr", declared(t, run, stdout).GetFee().String())
	})

	t.Run("gas prices price gas and the chain's tax rides the fee", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax, base: transferred}
		run, stdout, stderr, err := execute(t, chain, "--gas", "200000", "--gas-prices", "0.1axdr")
		require.NoError(t, err)
		require.Equal(t, 1, run.runs)
		fee := declared(t, run, stdout)
		require.Equal(t, "22002axdr", fee.GetFee().String())
		require.Equal(t, uint64(200_000), fee.GetGas())
		require.Equal(t,
			"fee 22002axdr: base fee 20000axdr + transfer tax 1axdr, stable legs declared with 10% headroom\n",
			stderr)
		// The chain priced the very message the command built, and was not
		// asked for a sheet the command's own gas price made needless.
		require.Equal(t, [][]string{{"/cosmos.bank.v1beta1.MsgSend"}}, chain.taxed)
		require.Zero(t, chain.sheets)
	})

	t.Run("auto gas takes the estimate and prices from the sheet", func(t *testing.T) {
		chain := &fakeChain{
			tax:       oneTax,
			base:      transferred,
			sheet:     sheet,
			gasUsed:   150_000,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1_000_000)),
		}
		run, stdout, stderr, err := execute(t, chain, "--gas", "auto")
		require.NoError(t, err)
		require.Equal(t, 1, run.runs)
		fee := declared(t, run, stdout)
		require.Equal(t, "16502axdr", fee.GetFee().String())
		require.Equal(t, uint64(150_000), fee.GetGas())
		require.Contains(t, stderr, "base fee 15000axdr")
		// The factory builds twice — once to simulate, once for real — and
		// the chain is asked once, for the transaction that carries gas.
		require.Len(t, chain.taxed, 1)
		require.Equal(t, 1, chain.sheets)
		require.Equal(t, []string{sender.String()}, chain.balances)
	})

	t.Run("an autocli message is priced as the bytes the chain decodes", func(t *testing.T) {
		// autocli hands the factory a dynamic message: no Go type to switch
		// on, and none needed — the chain decodes the bytes, and the payer is
		// read from the message's signer.
		dynamic := dynamicpb.NewMessage((&bankv1beta1.MsgSend{}).ProtoReflect().Descriptor())
		encoded, err := gogoproto.Marshal(msg)
		require.NoError(t, err)
		require.NoError(t, protov2.Unmarshal(encoded, dynamic))
		chain := &fakeChain{
			tax:       oneTax,
			base:      transferred,
			sheet:     sheet,
			gasUsed:   150_000,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1_000_000)),
		}
		run, stdout, _, err := executeMsg(t, chain, dynamic, "--gas", "auto")
		require.NoError(t, err)
		require.Equal(t, "16502axdr", declared(t, run, stdout).GetFee().String())
		require.Equal(t, [][]string{{"/cosmos.bank.v1beta1.MsgSend"}}, chain.taxed)
		require.Equal(t, []string{sender.String()}, chain.balances)
	})

	t.Run("a granter's balance is the one inspected", func(t *testing.T) {
		granter := sdk.AccAddress("granter")
		chain := &fakeChain{
			tax:       oneTax,
			base:      transferred,
			sheet:     sheet,
			spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1_000_000)),
		}
		_, _, _, err := execute(t, chain, "--gas", "200000", "--fee-granter", granter.String())
		require.NoError(t, err)
		require.Equal(t, []string{granter.String()}, chain.balances)
	})

	t.Run("a tip rides NOAH", func(t *testing.T) {
		run, stdout, stderr, err := execute(t, &fakeChain{tax: oneTax},
			"--gas", "200000", "--gas-prices", "0.1axdr", "--tip", "1000anoah")
		require.NoError(t, err)
		require.Equal(t, "1000anoah,22002axdr", declared(t, run, stdout).GetFee().String())
		require.Contains(t, stderr, "; tip 1000anoah")
	})

	t.Run("a tip in anything but NOAH is refused", func(t *testing.T) {
		run, _, _, err := execute(t, &fakeChain{}, "--gas", "100", "--tip", "1axdr")
		require.ErrorContains(t, err, "tips are paid in anoah")
		require.Zero(t, run.runs)
	})

	t.Run("offline refuses without an explicit fee", func(t *testing.T) {
		run, _, _, err := execute(t, &fakeChain{},
			"--offline", "--account-number", "1", "--sequence", "1",
			"--gas", "200000", "--gas-prices", "0.1axdr")
		require.ErrorContains(t, err, "offline, the transfer tax cannot be priced; provide --fees")
		require.Equal(t, 1, run.runs)
	})

	t.Run("nothing covering the fee refuses the command", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax, base: transferred, sheet: sheet}
		_, _, _, err := execute(t, chain, "--gas", "200000")

		// The refusal is raised where the built transaction is read, which
		// has no error to return, and reaches the command line all the same.
		require.ErrorContains(t, err, "no spendable balance covers the gas fee")
	})

	t.Run("a command that builds no transaction is untouched", func(t *testing.T) {
		// gov's draft-proposal carries the transaction flags, prompts for a
		// proposal and writes it to a file. It reaches no builder, so nothing
		// is priced and nothing stands between it and its output.
		chain := &fakeChain{tax: oneTax}
		var stdout bytes.Buffer
		cmd := &cobra.Command{Use: "draft-proposal", RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println("drafted")
			return nil
		}}
		flags.AddTxFlagsToCmd(cmd)
		PriceTransactions(cmd)
		cmd.SetOut(&stdout)
		cmd.SetContext(context.Background())
		require.NoError(t, sdkclient.SetCmdClientContext(cmd, connect(t, chain)))
		cmd.SetArgs([]string{"--gas", "200000"})

		require.NoError(t, cmd.Execute())
		require.Equal(t, "drafted\n", stdout.String())
		require.Empty(t, chain.taxed)
	})

	t.Run("aux reaches no builder", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax}
		run, _, _, _ := execute(t, chain, "--aux", "--gas", "100")

		// The SDK's aux path builds its signer data on its own builder, never
		// the TxConfig's; it fails here for want of a keyring, which is
		// beside the point that nothing was priced for it.
		require.Equal(t, 1, run.runs)
		require.Empty(t, chain.taxed)
	})
}

// TestPricingTxBuilder pins the seam itself: a builder settles its fee on the
// first read of what it built, whichever read that is, from what the built
// transaction carries.
func TestPricingTxBuilder(t *testing.T) {
	encoding := moduletestutil.MakeTestEncodingConfig()
	banktypes.RegisterInterfaces(encoding.InterfaceRegistry)
	send := func(amount int64) sdk.Msg {
		return banktypes.NewMsgSend(
			sdk.AccAddress("sender"),
			sdk.AccAddress("recipient"),
			sdk.NewCoins(sdk.NewInt64Coin("axdr", amount)),
		)
	}
	build := func(t *testing.T, chain *fakeChain) sdkclient.TxConfig {
		t.Helper()
		return pricingTxConfig{TxConfig: encoding.TxConfig, pricer: &pricer{clientCtx: connect(t, chain), out: io.Discard}}
	}

	t.Run("a signature settles the fee, and reading it again asks nothing", func(t *testing.T) {
		// A signature comes before any read when the SDK was told not to
		// confirm, and the sign bytes are taken from the transaction after it.
		chain := &fakeChain{tax: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))}
		builder := build(t, chain).NewTxBuilder()
		require.NoError(t, builder.SetMsgs(send(100)))
		builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("anoah", 25)))
		builder.SetGasLimit(100)
		require.NoError(t, builder.SetSignatures())

		require.Equal(t, "25anoah,2axdr", builder.GetTx().GetFee().String())
		require.Equal(t, "25anoah,2axdr", builder.GetTx().GetFee().String())
		require.Len(t, chain.taxed, 1)
	})

	t.Run("a transaction with no gas declares nothing and asks nothing", func(t *testing.T) {
		// The transaction the factory simulates for an estimate is one of
		// these; the build that follows it settles the fee for real.
		chain := &fakeChain{tax: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))}
		builder := build(t, chain).NewTxBuilder()
		require.NoError(t, builder.SetMsgs(send(100)))
		builder.SetGasLimit(0)

		require.True(t, builder.GetTx().GetFee().IsZero())
		require.Empty(t, chain.taxed)
	})

	t.Run("each transaction is priced for its own messages", func(t *testing.T) {
		// A command may build more than one — withdraw-all-rewards splits by
		// --max-msgs — and each is asked about for what it carries.
		chain := &fakeChain{tax: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))}
		txConfig := build(t, chain)
		for _, amount := range []int64{100, 200} {
			builder := txConfig.NewTxBuilder()
			require.NoError(t, builder.SetMsgs(send(amount)))
			builder.SetGasLimit(100)
			builder.GetTx()
		}

		require.Len(t, chain.taxed, 2)
	})
}

// TestPayerOf pins whose balance the fee is checked against, and whether the
// transfer counts against it: the first signer by default, else the payer or
// granter the transaction names — neither the account the messages draw on.
func TestPayerOf(t *testing.T) {
	encoding := moduletestutil.MakeTestEncodingConfig()
	banktypes.RegisterInterfaces(encoding.InterfaceRegistry)
	sender, other := sdk.AccAddress("sender"), sdk.AccAddress("other")
	tests := []struct {
		name  string
		set   func(sdkclient.TxBuilder)
		payer sdk.AccAddress
		sends bool
	}{
		{"the first signer pays and sends", func(sdkclient.TxBuilder) {}, sender, true},
		{"a named payer pays and does not send", func(b sdkclient.TxBuilder) { b.SetFeePayer(other) }, other, false},
		{"a granter pays and does not send", func(b sdkclient.TxBuilder) { b.SetFeeGranter(other) }, other, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder := encoding.TxConfig.NewTxBuilder()
			require.NoError(t, builder.SetMsgs(banktypes.NewMsgSend(sender, other, sdk.NewCoins())))
			tc.set(builder)

			payer, sends := payerOf(builder.GetTx())
			require.Equal(t, tc.payer, payer)
			require.Equal(t, tc.sends, sends)
		})
	}
}
