package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// The export semantics themselves are pinned in app/export_test.go, against
// ExportAppStateAndValidators. What these hold is the layer above it: the
// command wrapper that turns --height, --for-zero-height, and
// --jail-allowed-addrs into that call, and builds the app the call runs on.
// A wrapper that drops an argument exports the wrong chain, and the app-level
// tests would still pass.

const testChainID = "ark-commands-test"

// newTestAppOptions builds the viper appExport insists on, populated with the
// keys DefaultBaseappOptions reads. Its snapshot store is created under home,
// so home has to be writable and per-test.
func newTestAppOptions(t *testing.T, home string) *viper.Viper {
	t.Helper()

	v := viper.New()
	v.Set(flags.FlagHome, home)
	v.Set(flags.FlagChainID, testChainID)
	v.Set(server.FlagPruning, "nothing")

	return v
}

// writeGenesisChainID writes the one field DefaultBaseappOptions reads back
// out of genesis when no chain id is configured.
func writeGenesisChainID(t *testing.T, home, chainID string) {
	t.Helper()

	configDir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(configDir, 0o750))
	bz, err := json.Marshal(map[string]any{"chain_id": chainID, "app_state": map[string]any{}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "genesis.json"), bz, 0o600))
}

// commitBlocks initialises a chain on db and commits count blocks, leaving the
// store in the state a running node's would be in.
func commitBlocks(t *testing.T, db *dbm.MemDB, home string, count int64) {
	t.Helper()

	arkApp := apptestutil.NewArkappWithCustomOptions(t, false, apptestutil.SetupOptions{
		Logger:  log.NewNopLogger(),
		DB:      db,
		AppOpts: simtestutil.NewAppOptionsWithFlagHome(home),
	})
	for height := int64(1); height <= count; height++ {
		_, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{Height: height})
		require.NoError(t, err)
		_, err = arkApp.Commit()
		require.NoError(t, err)
	}
	// The Wasm VM holds an exclusive lock on the home's cache directory, so
	// the app has to go before another opens the same home.
	require.NoError(t, arkApp.Close())
}

func TestNewAppBuildsTheChain(t *testing.T) {
	home := t.TempDir()

	application := newApp(log.NewNopLogger(), dbm.NewMemDB(), newTestAppOptions(t, home))
	t.Cleanup(func() { _ = application.Close() })

	arkApp, ok := application.(*app.ArkApp)
	require.True(t, ok, "the server must be handed an ArkApp")
	require.Equal(t, testChainID, arkApp.ChainID())
}

// TestNewAppTakesTheChainIDFromGenesis covers the path a node actually starts
// on: no --chain-id, so the id comes from the home's genesis file. Getting it
// wrong is not a startup failure, it is a node signing for another chain.
func TestNewAppTakesTheChainIDFromGenesis(t *testing.T) {
	home := t.TempDir()
	writeGenesisChainID(t, home, "ark-from-genesis")

	options := newTestAppOptions(t, home)
	options.Set(flags.FlagChainID, "")

	application := newApp(log.NewNopLogger(), dbm.NewMemDB(), options)
	t.Cleanup(func() { _ = application.Close() })

	require.Equal(t, "ark-from-genesis", application.(*app.ArkApp).ChainID())
}

func TestAppExportRefusesNonViperOptions(t *testing.T) {
	_, err := appExport(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		-1,
		false,
		nil,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		nil,
	)

	require.ErrorContains(t, err, "appOpts is not viper.Viper")
}

// TestAppExportContinuesFromTheLatestHeight holds the -1 case to the same
// continuation contract app/export_test.go holds the app to: the exported
// genesis starts one past the last committed height.
func TestAppExportContinuesFromTheLatestHeight(t *testing.T) {
	db := dbm.NewMemDB()
	home := t.TempDir()
	commitBlocks(t, db, home, 2)

	exported, err := appExport(log.NewNopLogger(), db, -1, false, nil, newTestAppOptions(t, t.TempDir()), nil)

	require.NoError(t, err)
	require.Equal(t, int64(3), exported.Height)
	require.NotEmpty(t, exported.AppState)
}

// TestAppExportLoadsARequestedHeight is the --height path: the store is loaded
// at that version rather than the latest, and the export continues from it. An
// ignored --height would export height 3 here, as the case above does.
func TestAppExportLoadsARequestedHeight(t *testing.T) {
	db := dbm.NewMemDB()
	home := t.TempDir()
	commitBlocks(t, db, home, 2)

	exported, err := appExport(log.NewNopLogger(), db, 1, false, nil, newTestAppOptions(t, t.TempDir()), nil)

	require.NoError(t, err)
	require.Equal(t, int64(2), exported.Height)
}

func TestAppExportReportsAnUnloadableHeight(t *testing.T) {
	db := dbm.NewMemDB()
	home := t.TempDir()
	commitBlocks(t, db, home, 1)

	_, err := appExport(log.NewNopLogger(), db, 99, false, nil, newTestAppOptions(t, t.TempDir()), nil)

	require.Error(t, err)
}

// TestAppExportPassesForZeroHeightThrough and the jail-list test below are
// what prove the flags reach the app rather than being dropped: each asserts a
// refusal only the app layer issues.
func TestAppExportPassesForZeroHeightThrough(t *testing.T) {
	db := dbm.NewMemDB()
	home := t.TempDir()
	commitBlocks(t, db, home, 1)

	_, err := appExport(log.NewNopLogger(), db, -1, true, nil, newTestAppOptions(t, t.TempDir()), nil)

	require.Error(t, err, "a zero-height export is refused on purpose")
}

func TestAppExportPassesJailAllowedAddrsThrough(t *testing.T) {
	db := dbm.NewMemDB()
	home := t.TempDir()
	commitBlocks(t, db, home, 1)

	_, err := appExport(
		log.NewNopLogger(),
		db,
		-1,
		false,
		[]string{"arkvaloper1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"},
		newTestAppOptions(t, t.TempDir()),
		nil,
	)

	require.Error(t, err, "an allow-list naming no known validator is refused")
}
