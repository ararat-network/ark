package cmd_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client/flags"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"ark/app"
	"ark/cmd/arkd/cmd"
)

func TestInitCmd(t *testing.T) {
	rootCmd := cmd.NewRootCmd()
	rootCmd.SetArgs([]string{
		"init",        // Test the init cmd
		"arkapp-test", // Moniker
		fmt.Sprintf("--%s=%s", cli.FlagOverwrite, "true"), // Overwrite genesis.json, in case it already exists
	})

	require.NoError(t, svrcmd.Execute(rootCmd, "", app.DefaultNodeHome))
}

func TestHomeFlagRegistration(t *testing.T) {
	homeDir := "/tmp/foo"

	rootCmd := cmd.NewRootCmd()
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
	rootCmd := cmd.NewRootCmd()

	swapCmd, args, err := rootCmd.Find([]string{"tx", "market", "swap"})
	require.NoError(t, err)
	require.Empty(t, args)
	require.Equal(t, "swap", swapCmd.Name())
	require.Equal(t, "swap [offer-coin] [ask-denom] [minimum-receive]", swapCmd.Use)

	swapSendCmd, args, err := rootCmd.Find([]string{"tx", "market", "swap-send"})
	require.NoError(t, err)
	require.Empty(t, args)
	require.Equal(t, "swap-send", swapSendCmd.Name())
	require.Equal(
		t,
		"swap-send [offer-coin] [ask-denom] [minimum-receive] [to-address]",
		swapSendCmd.Use,
	)
}
