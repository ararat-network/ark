package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/version"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// adjustExportCommand hides unsupported zero-height export, describes the validator allowlist
// accurately, and assembles genesis with all ABCI consensus parameters.
func adjustExportCommand(rootCmd *cobra.Command, exporter servertypes.AppExporter) {
	exportCmd, _, err := rootCmd.Find([]string{"export"})
	if err != nil {
		panic(err)
	}
	if err := exportCmd.Flags().MarkHidden(server.FlagForZeroHeight); err != nil {
		panic(err)
	}
	if f := exportCmd.Flags().Lookup(server.FlagJailAllowedAddrs); f != nil {
		f.Usage = "Comma-separated operator addresses to keep in the exported validator set; every other validator is jailed"
	}
	exportCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runExport(cmd, exporter)
	}
}

// runExport is the SDK export command's run with continuationGenesis in
// place of genutil's consensus block: the same flags, the same output.
func runExport(cmd *cobra.Command, exporter servertypes.AppExporter) error {
	serverCtx := server.GetServerContextFromCmd(cmd)
	config := serverCtx.Config
	homeDir, _ := cmd.Flags().GetString(flags.FlagHome)
	config.SetRoot(homeDir)
	if _, err := os.Stat(config.GenesisFile()); os.IsNotExist(err) {
		return err
	}
	db, err := dbm.NewDB("application", server.GetAppDBBackend(serverCtx.Viper), filepath.Join(config.RootDir, "data"))
	if err != nil {
		return err
	}

	height, _ := cmd.Flags().GetInt64(server.FlagHeight)
	forZeroHeight, _ := cmd.Flags().GetBool(server.FlagForZeroHeight)
	jailAllowedAddrs, _ := cmd.Flags().GetStringSlice(server.FlagJailAllowedAddrs)
	modulesToExport, _ := cmd.Flags().GetStringSlice(server.FlagModulesToExport)
	outputDocument, _ := cmd.Flags().GetString(flags.FlagOutputDocument)

	exported, err := exporter(serverCtx.Logger, db, height, forZeroHeight, jailAllowedAddrs, serverCtx.Viper, modulesToExport)
	if err != nil {
		return fmt.Errorf("error exporting state: %w", err)
	}
	base, err := genutiltypes.AppGenesisFromFile(config.GenesisFile())
	if err != nil {
		return err
	}
	genesis := continuationGenesis(base, exported)
	if outputDocument != "" {
		return genesis.SaveAs(outputDocument)
	}
	out, err := json.Marshal(genesis)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(out)
	return err
}

// continuationGenesis preserves exported state and every consensus parameter, including ABCI
// vote-extension settings omitted by genutil's constructor.
func continuationGenesis(base *genutiltypes.AppGenesis, exported servertypes.ExportedApp) *genutiltypes.AppGenesis {
	params := cmttypes.ConsensusParamsFromProto(exported.ConsensusParams)
	base.AppName = version.AppName
	base.AppVersion = version.Version
	base.AppState = exported.AppState
	base.InitialHeight = exported.Height
	base.Consensus = &genutiltypes.ConsensusGenesis{
		Validators: exported.Validators,
		Params:     &params,
	}
	return base
}
