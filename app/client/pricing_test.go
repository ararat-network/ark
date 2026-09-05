package client

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// sdkRun mimics a module transaction command's RunE: under --generate-only
// it prints the unsigned transaction the SDK would — the messages, the gas
// the flags settle on with the estimate standing in for auto, and a fee
// from --fees or else --gas-prices — and otherwise records what it would
// have broadcast with.
type sdkRun struct {
	txConfig sdkclient.TxConfig
	msgs     []sdk.Msg
	estimate uint64

	runs                 int
	fees, gasPrices, gas string
	generateOnly         bool
}

func (r *sdkRun) runE(cmd *cobra.Command, _ []string) error {
	r.runs++
	flagSet := cmd.Flags()
	r.gas, _ = flagSet.GetString(flags.FlagGas)
	r.fees, _ = flagSet.GetString(flags.FlagFees)
	r.gasPrices, _ = flagSet.GetString(flags.FlagGasPrices)
	r.generateOnly, _ = flagSet.GetBool(flags.FlagGenerateOnly)
	if !r.generateOnly {
		return nil
	}

	setting, err := flags.ParseGasSetting(r.gas)
	if err != nil {
		return err
	}
	gas := setting.Gas
	if setting.Simulate {
		gas = r.estimate
	}
	builder := r.txConfig.NewTxBuilder()
	if err := builder.SetMsgs(r.msgs...); err != nil {
		return err
	}
	builder.SetGasLimit(gas)
	switch {
	case r.fees != "":
		fee, err := sdk.ParseCoinsNormalized(r.fees)
		if err != nil {
			return err
		}
		builder.SetFeeAmount(fee)
	case r.gasPrices != "":
		prices, err := sdk.ParseDecCoins(r.gasPrices)
		if err != nil {
			return err
		}
		builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(
			prices[0].Denom, prices[0].Amount.MulInt64(int64(gas)).Ceil().RoundInt(),
		)))
	}
	encoded, err := r.txConfig.TxJSONEncoder()(builder.GetTx())
	if err != nil {
		return err
	}
	return sdkclient.GetClientContextFromCmd(cmd).PrintString(string(encoded) + "\n")
}

// completeFixture is a wrapped command over the fake chain, its stdout and
// stderr captured, with sdkRun recording what the command saw. The stored
// context is the persistent one a command starts from: --from, --offline,
// and the rest arrive as flags, as they do for arkd.
func completeFixture(t *testing.T, chain *fakeChain, msgs ...sdk.Msg) (*cobra.Command, *sdkRun, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	encoding := moduletestutil.MakeTestEncodingConfig()
	banktypes.RegisterInterfaces(encoding.InterfaceRegistry)
	run := &sdkRun{txConfig: encoding.TxConfig, msgs: msgs, estimate: 150_000}
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{Use: "send", RunE: run.runE}
	flags.AddTxFlagsToCmd(cmd)
	CompleteFees(cmd)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	clientCtx := connect(t, chain, nil).
		WithTxConfig(encoding.TxConfig).
		WithCodec(encoding.Codec).
		WithInterfaceRegistry(encoding.InterfaceRegistry).
		WithOutput(&stdout)
	require.NoError(t, sdkclient.SetCmdClientContext(cmd, clientCtx))
	return cmd, run, &stdout, &stderr
}

// TestCompleteFees pins the two-pass wrapper: the first pass runs under
// --generate-only and is captured, the second runs with the completed fee
// and the settled gas; an explicit fee and --aux run once; the parts of the
// fee are printed to stderr.
func TestCompleteFees(t *testing.T) {
	sender := sdk.AccAddress("sender")
	msg := banktypes.NewMsgSend(sender, sdk.AccAddress("recipient"), sdk.NewCoins(sdk.NewInt64Coin("axdr", 100)))
	sheet := treasurytypes.QueryGasPricesResponse{
		GasPrices:         []treasurytypes.GasPrice{{Denom: "anoah", GasPrice: sdkmath.LegacyMustNewDecFromStr("0.025")}},
		ReferenceGasPrice: treasurytypes.GasPrice{Denom: "axdr", GasPrice: sdkmath.LegacyMustNewDecFromStr("0.1")},
	}
	oneTax := sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))
	// 200k gas at 0.1axdr is 20,000; with the one of tax, lifted, 22,002.
	// The estimate is 150k: 15,000, with the tax lifted 16,502.

	execute := func(t *testing.T, chain *fakeChain, args ...string) (*sdkRun, string, string, error) {
		t.Helper()
		cmd, run, stdout, stderr := completeFixture(t, chain, msg)
		cmd.SetArgs(append([]string{"--from", sender.String()}, args...))
		err := cmd.Execute()
		return run, stdout.String(), stderr.String(), err
	}

	t.Run("an explicit fee runs once, as declared", func(t *testing.T) {
		run, _, stderr, err := execute(t, &fakeChain{tax: oneTax}, "--fees", "5axdr", "--gas", "100")
		require.NoError(t, err)
		require.Equal(t, 1, run.runs)
		require.Equal(t, "5axdr", run.fees)
		require.False(t, run.generateOnly)
		require.Empty(t, stderr)
	})

	t.Run("an explicit fee takes the tip", func(t *testing.T) {
		run, _, _, err := execute(t, &fakeChain{tax: oneTax}, "--fees", "5axdr", "--tip", "7anoah")
		require.NoError(t, err)
		require.Equal(t, 1, run.runs)
		require.Equal(t, "7anoah,5axdr", run.fees)
	})

	t.Run("gas prices price gas and the chain's tax rides the fee", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax}
		run, _, stderr, err := execute(t, chain, "--gas", "200000", "--gas-prices", "0.1axdr")
		require.NoError(t, err)
		require.Equal(t, 2, run.runs)
		require.Equal(t, "22002axdr", run.fees)
		require.Empty(t, run.gasPrices)
		require.Equal(t, "200000", run.gas)
		require.False(t, run.generateOnly)
		require.Equal(t,
			"fee 22002axdr: base fee 20000axdr + transfer tax 1axdr, stable legs declared with 10% headroom\n",
			stderr)
		// The chain priced the very message the command built.
		require.Equal(t, [][]string{{"/cosmos.bank.v1beta1.MsgSend"}}, chain.taxed)
	})

	t.Run("auto gas takes the estimate and prices from the sheet", func(t *testing.T) {
		chain := &fakeChain{tax: oneTax, sheet: sheet, spendable: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1_000_000))}
		run, _, stderr, err := execute(t, chain, "--gas", "auto")
		require.NoError(t, err)
		require.Equal(t, 2, run.runs)
		require.Equal(t, "16502axdr", run.fees)
		require.Equal(t, "150000", run.gas)
		require.Contains(t, stderr, "base fee 15000axdr")
	})

	t.Run("a tip rides NOAH", func(t *testing.T) {
		run, _, stderr, err := execute(t, &fakeChain{tax: oneTax},
			"--gas", "200000", "--gas-prices", "0.1axdr", "--tip", "1000anoah")
		require.NoError(t, err)
		require.Equal(t, "1000anoah,22002axdr", run.fees)
		require.Contains(t, stderr, "; tip 1000anoah")
	})

	t.Run("generate-only stays generate-only, completed", func(t *testing.T) {
		run, stdout, _, err := execute(t, &fakeChain{tax: oneTax},
			"--generate-only", "--gas", "200000", "--gas-prices", "0.1axdr")
		require.NoError(t, err)
		require.Equal(t, 2, run.runs)
		require.True(t, run.generateOnly)
		require.Equal(t, "22002axdr", run.fees)
		// The first pass was captured; only the second reached the output,
		// carrying the completed fee.
		require.Equal(t, 1, strings.Count(stdout, `"body"`))
		require.Contains(t, stdout, `"22002"`)
	})

	t.Run("a tip in anything but NOAH is refused", func(t *testing.T) {
		run, _, _, err := execute(t, &fakeChain{}, "--gas", "100", "--tip", "1axdr")
		require.ErrorContains(t, err, "tips are paid in anoah")
		require.Zero(t, run.runs)
	})

	t.Run("offline refuses a transfer that may owe tax", func(t *testing.T) {
		run, _, _, err := execute(t, &fakeChain{}, "--offline", "--gas", "200000", "--gas-prices", "0.1axdr")
		require.ErrorContains(t, err, "provide --fees carrying it")
		require.Equal(t, 1, run.runs)
	})

	t.Run("aux is left alone", func(t *testing.T) {
		run, _, _, err := execute(t, &fakeChain{}, "--aux", "--gas", "100")
		require.NoError(t, err)
		require.Equal(t, 1, run.runs)
		require.Empty(t, run.fees)
	})
}
