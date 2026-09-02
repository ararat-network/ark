package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/sidecar"
)

func TestRootCmdWithoutArgsShowsHelp(t *testing.T) {
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(nil)

	err := cmd.Execute()

	require.NoError(t, err)
	require.Contains(t, out.String(), "start")
	require.Contains(t, out.String(), "validate")
	require.Contains(t, out.String(), "version")
}

func TestRootCmdOwnsErrorAndUsageSilencing(t *testing.T) {
	cmd := NewRootCmd()

	require.True(t, cmd.SilenceErrors)
	require.True(t, cmd.SilenceUsage)

	startCmd, _, err := cmd.Find([]string{"start"})
	require.NoError(t, err)
	require.False(t, startCmd.SilenceUsage)

	validateCmd, _, err := cmd.Find([]string{"validate"})
	require.NoError(t, err)
	require.False(t, validateCmd.SilenceUsage)

	initCmd, _, err := cmd.Find([]string{"init"})
	require.NoError(t, err)
	require.False(t, initCmd.SilenceUsage)

	configCmd, _, err := cmd.Find([]string{"config"})
	require.NoError(t, err)
	require.False(t, configCmd.SilenceUsage)

	configValidateCmd, _, err := cmd.Find([]string{"config", "validate"})
	require.NoError(t, err)
	require.False(t, configValidateCmd.SilenceUsage)

	configReloadCmd, _, err := cmd.Find([]string{"config", "reload"})
	require.NoError(t, err)
	require.False(t, configReloadCmd.SilenceUsage)
}

func TestRootCmdOwnsConfigFlagOnly(t *testing.T) {
	cmd := NewRootCmd()

	require.NotNil(t, cmd.PersistentFlags().Lookup(flagConfig))
	require.Nil(t, cmd.Flags().Lookup(flagAddress))
	require.Nil(t, cmd.Flags().Lookup(flagAdminAddress))
	require.Nil(t, cmd.Flags().Lookup(flagMetrics))
}

func TestRootCmdDefaultsConfigFlagToHomeDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	cmd := NewRootCmd()
	flag := cmd.PersistentFlags().Lookup(flagConfig)

	require.NotNil(t, flag)
	require.Equal(t, filepath.Join(homeDir, ".ark", "pricefeed", "config.json"), flag.DefValue)
	require.Equal(t, flag.DefValue, flag.Value.String())
}

func TestRootCmdExposesValidateInitAndConfigCommands(t *testing.T) {
	cmd := NewRootCmd()

	validateCmd, _, err := cmd.Find([]string{"validate"})
	require.NoError(t, err)
	require.NotNil(t, validateCmd)
	require.Equal(t, "validate", validateCmd.Name())

	initCmd, _, err := cmd.Find([]string{"init"})
	require.NoError(t, err)
	require.NotNil(t, initCmd)
	require.Equal(t, "init", initCmd.Name())

	configCmd, _, err := cmd.Find([]string{"config"})
	require.NoError(t, err)
	require.NotNil(t, configCmd)
	require.Equal(t, "config", configCmd.Name())
}

func TestNewVersionCmdPrintsVersion(t *testing.T) {
	cmd := newVersionCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetArgs(nil)

	err := cmd.Execute()

	require.NoError(t, err)
	require.Equal(t, sidecar.Version()+"\n", out.String())
	require.NotEmpty(t, strings.TrimSpace(out.String()))
}
