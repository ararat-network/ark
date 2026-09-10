package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/config"
)

func TestInitCmdWritesDefaultRuntimeConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "pricefeed.toml")
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--" + flagConfig, cfgPath, "init"})

	err := cmd.Execute()

	require.NoError(t, err)
	require.Empty(t, out.String())

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestInitCmdRefusesToOverwriteWithoutTheFlag(t *testing.T) {
	cfgPath := writeConfig(t, validConfigTOML())
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--" + flagConfig, cfgPath, "init"})

	err := cmd.Execute()

	require.ErrorContains(t, err, "already exists")
}

// A pre-existing file's mode is replaced along with its contents, and the
// temporary file the replacement went through is gone.
func TestInitCmdOverwriteReplacesFileOwnerOnly(t *testing.T) {
	cfgPath := writeConfig(t, validConfigTOML())
	require.NoError(t, os.Chmod(cfgPath, 0o644))
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"--" + flagConfig, cfgPath, "init", "--" + flagOverwrite})

	err := cmd.Execute()

	require.NoError(t, err)
	info, err := os.Stat(cfgPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	_, err = config.Load(cfgPath)
	require.NoError(t, err)
	entries, err := os.ReadDir(filepath.Dir(cfgPath))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
