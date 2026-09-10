package cmd

import (
	"bytes"
	"io"
	"os"
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

func TestRootCmdDefaultsLogFlags(t *testing.T) {
	flags := NewRootCmd().PersistentFlags()

	require.Equal(t, defaultLogLevel, flags.Lookup(flagLogLevel).DefValue)
	require.Equal(t, defaultLogFormat, flags.Lookup(flagLogFormat).DefValue)
}

// The logger is built before any command runs, so a bad log flag fails even
// a command that never logs.
func TestRootCmdRejectsInvalidLogFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "level", args: []string{"--" + flagLogLevel, "chatty", "version"}, wantErr: "invalid log level"},
		{name: "format", args: []string{"--" + flagLogFormat, "yaml", "version"}, wantErr: `unsupported log format "yaml"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
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

func TestNewLoggerRejectsInvalidLevel(t *testing.T) {
	logger, err := newLogger("not-a-level", logFormatPlain)

	require.Nil(t, logger)
	require.ErrorContains(t, err, "invalid log level")
}

func TestNewLoggerRejectsUnknownFormat(t *testing.T) {
	logger, err := newLogger(defaultLogLevel, "yaml")

	require.Nil(t, logger)
	require.ErrorContains(t, err, `unsupported log format "yaml"`)
}

// The log label is the metrics service name, so the two signals join.
func TestNewLoggerAddsServiceLabel(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	require.NoError(t, err)
	oldStderr := os.Stderr
	os.Stderr = writeEnd
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = readEnd.Close()
		_ = writeEnd.Close()
	})

	logger, err := newLogger(defaultLogLevel, logFormatPlain)
	require.NoError(t, err)

	logger.Info("pricefeed command logger labels")
	require.NoError(t, writeEnd.Close())
	output, err := io.ReadAll(readEnd)
	require.NoError(t, err)

	require.Contains(t, string(output), "service="+serviceName)
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
