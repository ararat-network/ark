package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/ararat-network/ark/app"
)

// These tests cover newApp: app construction from the server's options.

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
