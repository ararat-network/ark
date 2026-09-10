package cmd

import (
	"errors"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"

	cmtcli "github.com/cometbft/cometbft/libs/cli"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/pruning"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"

	"github.com/ararat-network/ark/app"
)

func initRootCmd(
	rootCmd *cobra.Command,
	txConfig client.TxConfig,
	basicManager module.BasicManager,
	manualBasics module.BasicManager,
) {
	cfg := sdk.GetConfig()
	cfg.Seal()

	rootCmd.AddCommand(
		genutilcli.InitCmd(basicManager, app.DefaultNodeHome),
		cmtcli.NewCompletionCmd(rootCmd, true),
		newTestnetCmd(basicManager, banktypes.GenesisBalancesIterator{}),
		debug.Cmd(),
		newConfigCmd(),
		pruning.Cmd(newApp, app.DefaultNodeHome),
		snapshot.Cmd(newApp),
	)

	run := &startRun{}
	server.AddCommandsWithStartCmdOptions(rootCmd, app.DefaultNodeHome, run.createApp, appExport, server.StartCmdOptions{
		PostSetup:           run.postSetup,
		PostSetupStandalone: run.postSetup,
	})
	adjustStartCommand(rootCmd, run)
	adjustExportCommand(rootCmd, appExport)

	queryCmd := newQueryCmd()
	txCmd := newTxCmd()
	manualBasics.AddQueryCommands(queryCmd)
	manualBasics.AddTxCommands(txCmd)

	// add keybase, auxiliary RPC, query, genesis, and tx child commands
	rootCmd.AddCommand(
		server.StatusCommand(),
		genutilcli.Commands(txConfig, basicManager, app.DefaultNodeHome),
		queryCmd,
		txCmd,
		keys.Commands(),
	)
}

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		newVoteExtensionsCmd(),
		rpc.ValidatorCommand(),
		rpc.WaitTxCmd(),
		server.QueryBlockCmd(),
		server.QueryBlocksCmd(),
		server.QueryBlockResultsCmd(),
		authcmd.QueryTxsByEventsCmd(),
		authcmd.QueryTxCmd(),
	)

	return cmd
}

// manualFeesAnnotation marks a tx command whose fee dressTxCommands leaves alone: it
// carries a finished transaction rather than building one.
const manualFeesAnnotation = "ark.fees"

func newTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetMultiSignBatchCmd(),
		authcmd.GetValidateSignaturesCommand(),
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
		authcmd.GetSimulateCmd(),
	)
	for _, utility := range cmd.Commands() {
		if utility.Annotations == nil {
			utility.Annotations = map[string]string{}
		}
		utility.Annotations[manualFeesAnnotation] = "manual"
	}

	return cmd
}

// newApp creates the application
func newApp(
	logger log.Logger,
	db dbm.DB,
	appOpts servertypes.AppOptions,
) servertypes.Application {
	baseappOptions := server.DefaultBaseappOptions(appOpts)
	return app.NewArkApp(
		logger, db, true,
		appOpts,
		baseappOptions...,
	)
}

// appExport creates a new arkapp (optionally at a given height) and exports state.
func appExport(
	logger log.Logger,
	db dbm.DB,
	height int64,
	forZeroHeight bool,
	jailAllowedAddrs []string,
	appOpts servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	var arkApp *app.ArkApp
	if height != -1 {
		arkApp = app.NewArkApp(logger, db, false, appOpts)

		if err := arkApp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, errors.Join(err, arkApp.Close())
		}
	} else {
		arkApp = app.NewArkApp(logger, db, true, appOpts)
	}

	exported, exportErr := arkApp.ExportAppStateAndValidators(forZeroHeight, jailAllowedAddrs, modulesToExport)
	return exported, errors.Join(exportErr, arkApp.Close())
}
