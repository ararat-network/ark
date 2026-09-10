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
	require.Contains(t, out.String(), "check")
	require.Contains(t, out.String(), "version")
}

// The root silences cobra's own output: main prints the error once, and a
// runtime failure is not a usage mistake.
func TestRootCmdSilencesCobraErrorOutput(t *testing.T) {
	cmd := NewRootCmd()

	require.True(t, cmd.SilenceErrors)
	require.True(t, cmd.SilenceUsage)
}

func TestRootCmdDefaultsConfigFlagToHomeDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	flag := NewRootCmd().PersistentFlags().Lookup(flagConfig)

	require.NotNil(t, flag)
	require.Equal(t, filepath.Join(homeDir, ".ark", "pricefeed", "pricefeed.toml"), flag.DefValue)
	require.Equal(t, flag.DefValue, flag.Value.String())
}

func TestRootCmdExposesSubcommands(t *testing.T) {
	root := NewRootCmd()

	for _, name := range []string{"start", "prices", "check", "init", "config", "version"} {
		t.Run(name, func(t *testing.T) {
			cmd, _, err := root.Find([]string{name})

			require.NoError(t, err)
			require.Equal(t, name, cmd.Name())
		})
	}
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
