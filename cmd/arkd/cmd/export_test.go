package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// TestContinuationGenesisKeepsEveryConsensusParam pins what the SDK's own
// export loses: the ABCI and version groups survive the trip through the
// genesis document, so a relaunch keeps vote extensions on.
func TestContinuationGenesisKeepsEveryConsensusParam(t *testing.T) {
	params := cmttypes.DefaultConsensusParams().ToProto()
	params.Abci.VoteExtensionsEnableHeight = 1
	params.Version.App = 7
	base := &genutiltypes.AppGenesis{
		ChainID:   "ark-export-test",
		Consensus: &genutiltypes.ConsensusGenesis{Params: cmttypes.DefaultConsensusParams()},
	}
	exported := servertypes.ExportedApp{
		AppState:        json.RawMessage(`{"bank":{}}`),
		Height:          42,
		ConsensusParams: params,
	}

	out, err := json.Marshal(continuationGenesis(base, exported))
	require.NoError(t, err)
	got, err := genutiltypes.AppGenesisFromReader(bytes.NewReader(out))
	require.NoError(t, err)

	require.Equal(t, int64(42), got.InitialHeight)
	require.Equal(t, int64(1), got.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
	require.Equal(t, uint64(7), got.Consensus.Params.Version.App)
	require.JSONEq(t, `{"bank":{}}`, string(got.AppState))
}

// TestRunExportKeepsStdoutForTheGenesis pins that `arkd export > genesis.json`
// is the document alone: the server logger writes to stdout, so the export
// hands its app a logger on stderr.
func TestRunExportKeepsStdoutForTheGenesis(t *testing.T) {
	home := t.TempDir()
	config := cmtcfg.DefaultConfig()
	config.SetRoot(home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o750))
	base := &genutiltypes.AppGenesis{
		ChainID:   "ark-export-test",
		Consensus: &genutiltypes.ConsensusGenesis{Params: cmttypes.DefaultConsensusParams()},
	}
	require.NoError(t, base.SaveAs(config.GenesisFile()))

	v := viper.New()
	v.Set(flags.FlagHome, home)
	// The server logger is the stdout one the SDK builds; nothing the export
	// does may write through it.
	var stdout, stderr bytes.Buffer
	serverCtx := server.NewContext(v, config, log.NewLogger(&stdout, log.ColorOption(false)))

	exporter := func(logger log.Logger, _ dbm.DB, _ int64, _ bool, _ []string, _ servertypes.AppOptions, _ []string) (servertypes.ExportedApp, error) {
		logger.Info("export noise")
		return servertypes.ExportedApp{
			AppState:        json.RawMessage(`{"bank":{}}`),
			Height:          9,
			ConsensusParams: cmttypes.DefaultConsensusParams().ToProto(),
		}, nil
	}
	cmd := &cobra.Command{
		Use:  "export",
		RunE: func(cmd *cobra.Command, _ []string) error { return runExport(cmd, exporter) },
	}
	cmd.Flags().String(flags.FlagHome, home, "")
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	ctx := context.WithValue(context.Background(), server.ServerContextKey, serverCtx)
	require.NoError(t, cmd.ExecuteContext(ctx))

	require.Truef(t, json.Valid(stdout.Bytes()), "stdout is not one JSON document:\n%s", stdout.String())
	got, err := genutiltypes.AppGenesisFromReader(bytes.NewReader(stdout.Bytes()))
	require.NoError(t, err)
	require.Equal(t, int64(9), got.InitialHeight)
	require.Contains(t, stderr.String(), "export noise")
	require.DirExists(t, filepath.Join(home, "data", "application.db"))
}
