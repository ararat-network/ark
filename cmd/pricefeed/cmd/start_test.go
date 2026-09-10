package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartCmdUsesStartOptionsDefaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "pricefeed.toml")
	options := startOptions{
		address:        defaultAddress,
		adminAddress:   defaultAdminAddress,
		metrics:        defaultMetrics,
		metricsAddress: defaultMetricsAddress,
		pprof:          defaultPprof,
		pprofAddress:   defaultPprofAddress,
		logLevel:       defaultLogLevel,
		logJSON:        defaultLogJSON,
	}

	startCmd := newStartCmd(&configPath)
	flags := startCmd.Flags()

	require.Equal(t, options.address, flags.Lookup(flagAddress).DefValue)
	require.Equal(t, options.adminAddress, flags.Lookup(flagAdminAddress).DefValue)
	require.Equal(t, "false", flags.Lookup(flagMetrics).DefValue)
	require.Equal(t, options.metricsAddress, flags.Lookup(flagMetricsAddress).DefValue)
	require.Equal(t, "false", flags.Lookup(flagPprof).DefValue)
	require.Equal(t, options.pprofAddress, flags.Lookup(flagPprofAddress).DefValue)
	require.Equal(t, options.logLevel, flags.Lookup(flagLogLevel).DefValue)
	require.Equal(t, "false", flags.Lookup(flagLogJSON).DefValue)
}

func TestStartCmdBooleanFlagsEnableByPresence(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "pricefeed.toml")
	flags := newStartCmd(&configPath).Flags()

	require.NoError(t, flags.Parse([]string{"--" + flagMetrics, "--" + flagPprof}))
	metrics, err := flags.GetBool(flagMetrics)
	require.NoError(t, err)
	pprof, err := flags.GetBool(flagPprof)
	require.NoError(t, err)

	require.True(t, metrics)
	require.True(t, pprof)
}

func TestNewLoggerRejectsInvalidLevel(t *testing.T) {
	logger, err := newLogger("not-a-level", false)

	require.Nil(t, logger)
	require.ErrorContains(t, err, "invalid log level")
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

	logger, err := newLogger("info", false)
	require.NoError(t, err)

	logger.Info("pricefeed command logger labels")
	require.NoError(t, writeEnd.Close())
	output, err := io.ReadAll(readEnd)
	require.NoError(t, err)

	require.Contains(t, string(output), "service="+serviceName)
}
