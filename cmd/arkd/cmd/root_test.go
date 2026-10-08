package cmd

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"github.com/ararat-network/ark/app"
	appclient "github.com/ararat-network/ark/app/client"
)

func TestInitCmd(t *testing.T) {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{
		"init",        // Test the init cmd
		"arkapp-test", // Moniker
		fmt.Sprintf("--%s=%s", genutilcli.FlagOverwrite, "true"), // Overwrite genesis.json, in case it already exists
		fmt.Sprintf("--%s=%s", flags.FlagHome, t.TempDir()),
	})

	require.NoError(t, svrcmd.Execute(rootCmd, "", app.DefaultNodeHome))
}

// TestStartValidatesBeforeRunning pins that start's own checks run in its
// PreRunE, on the real command: a config.toml the app pool cannot mirror is
// refused with nothing started.
func TestStartValidatesBeforeRunning(t *testing.T) {
	home := t.TempDir()
	for _, args := range [][]string{
		{"init", "start-test"},
		{"config", "set", "config", "mempool.type", "nop"},
	} {
		rootCmd := NewRootCmd()
		rootCmd.SetArgs(append(args, fmt.Sprintf("--%s=%s", flags.FlagHome, home)))
		require.NoError(t, svrcmd.Execute(rootCmd, "", app.DefaultNodeHome), args)
	}

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{"start", fmt.Sprintf("--%s=%s", flags.FlagHome, home)})
	require.ErrorContains(t, svrcmd.Execute(rootCmd, "", app.DefaultNodeHome), "mempool.type flood")
}

func TestHomeFlagRegistration(t *testing.T) {
	homeDir := "/tmp/foo"

	rootCmd := NewRootCmd()
	rootCmd.SetArgs([]string{
		"query",
		fmt.Sprintf("--%s", flags.FlagHome),
		homeDir,
	})

	require.NoError(t, svrcmd.Execute(rootCmd, "", app.DefaultNodeHome))

	flag := rootCmd.PersistentFlags().Lookup(flags.FlagHome)
	require.NotNil(t, flag)
	require.Equal(t, app.DefaultNodeHome, flag.DefValue)
}

func TestMarketTxCommands(t *testing.T) {
	rootCmd := NewRootCmd()

	sellCmd, args, err := rootCmd.Find([]string{"tx", "market", "sell"})
	require.NoError(t, err)
	require.Empty(t, args)
	require.Equal(t, "sell", sellCmd.Name())
	require.Equal(t, "sell [offer-coin] [ask-denom] [minimum-receive]", sellCmd.Use)

	buyCmd, args, err := rootCmd.Find([]string{"tx", "market", "buy"})
	require.NoError(t, err)
	require.Empty(t, args)
	require.Equal(t, "buy", buyCmd.Name())
	require.Equal(t, "buy [ask-coin] [offer-denom] [maximum-offer]", buyCmd.Use)
}

// TestTxCommandsArePriced pins the pricing walk through the one
// thing it changes on a command's surface: the --tip flag's help. A module
// transaction command carries Ark's text; an auth utility, which carries a
// finished transaction rather than building one, keeps the SDK's.
func TestTxCommandsArePriced(t *testing.T) {
	rootCmd := NewRootCmd()

	for _, path := range [][]string{
		{"tx", "bank", "send"},
		{"tx", "wasm", "execute"},
		{"tx", "market", "sell"},
	} {
		command, _, err := rootCmd.Find(path)
		require.NoError(t, err)
		require.Equal(t, appclient.TipFlagUsage, command.Flags().Lookup(flags.FlagTip).Usage, path)
	}

	signCmd, _, err := rootCmd.Find([]string{"tx", "sign"})
	require.NoError(t, err)
	require.NotEqual(t, appclient.TipFlagUsage, signCmd.Flags().Lookup(flags.FlagTip).Usage)
}

// TestExportCommandRelaunchFlags pins the export command's relaunch surface:
// the zero-height flag is hidden because the app refuses it, and the
// allow-list flag says what it does rather than what upstream's help claims.
func TestExportCommandRelaunchFlags(t *testing.T) {
	rootCmd := NewRootCmd()
	exportCmd, _, err := rootCmd.Find([]string{"export"})
	require.NoError(t, err)
	require.Equal(t, "export", exportCmd.Name())

	zeroHeight := exportCmd.Flags().Lookup(server.FlagForZeroHeight)
	require.NotNil(t, zeroHeight)
	require.True(t, zeroHeight.Hidden)

	allowed := exportCmd.Flags().Lookup(server.FlagJailAllowedAddrs)
	require.NotNil(t, allowed)
	require.Contains(t, allowed.Usage, "every other validator is jailed")
}

// TestTxCommandsDefaultGasAdjustment pins the CLI-wide default: every tx
// command, autocli-generated ones included, carries it as both the shown
// default and the live value, and commands without the flag are untouched.
func TestTxCommandsDefaultGasAdjustment(t *testing.T) {
	rootCmd := NewRootCmd()

	for _, path := range [][]string{{"tx", "bank", "send"}, {"tx", "gov", "vote"}, {"tx", "market", "sell"}} {
		sub, _, err := rootCmd.Find(path)
		require.NoError(t, err)
		flag := sub.Flags().Lookup(flags.FlagGasAdjustment)
		require.NotNil(t, flag, "%v", path)
		want := strconv.FormatFloat(appclient.DefaultGasAdjustment, 'f', -1, 64)
		require.Equal(t, want, flag.DefValue, "%v", path)
		require.Equal(t, want, flag.Value.String(), "%v", path)
	}

	query, _, err := rootCmd.Find([]string{"query", "bank", "balances"})
	require.NoError(t, err)
	require.Nil(t, query.Flags().Lookup(flags.FlagGasAdjustment))
}

// TestServerCommandsRegistered pins the SDK server commands the tree carries
// beyond what AddCommandsWithStartCmdOptions adds on its own.
func TestServerCommandsRegistered(t *testing.T) {
	rootCmd := NewRootCmd()
	for _, path := range [][]string{
		{"module-hash-by-height"},
		{"in-place-testnet"},
		{"rollback"},
		{"comet", "bootstrap-state"},
	} {
		cmd, args, err := rootCmd.Find(path)
		require.NoError(t, err, path)
		require.Empty(t, args, path)
		require.Equal(t, path[len(path)-1], cmd.Name(), path)
	}
}
