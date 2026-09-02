package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartCmdOwnsProcessFlags(t *testing.T) {
	cmd := NewRootCmd()
	startCmd, _, err := cmd.Find([]string{"start"})

	require.NoError(t, err)
	require.NotNil(t, startCmd)
	require.Nil(t, startCmd.Flags().Lookup(flagConfig))
	require.NotNil(t, startCmd.Flags().Lookup(flagAddress))
	require.NotNil(t, startCmd.Flags().Lookup(flagAdminAddress))
	require.NotNil(t, startCmd.Flags().Lookup(flagMetrics))
	require.NotNil(t, startCmd.Flags().Lookup(flagMetricsAddress))
	require.NotNil(t, startCmd.Flags().Lookup(flagPprof))
	require.NotNil(t, startCmd.Flags().Lookup(flagLogLevel))
	require.Nil(t, startCmd.Flags().Lookup("host"))
	require.Nil(t, startCmd.Flags().Lookup("port"))
	require.Nil(t, startCmd.Flags().Lookup("admin-host"))
	require.Nil(t, startCmd.Flags().Lookup("admin-port"))
	require.Nil(t, startCmd.Flags().Lookup("metrics-enabled"))
	require.Nil(t, startCmd.Flags().Lookup("metrics-prometheus-address"))
	require.Nil(t, startCmd.Flags().Lookup("run-pprof"))
	require.Nil(t, startCmd.Flags().Lookup("pprof-port"))
	require.Nil(t, startCmd.Flags().Lookup("log-std-out-level"))
	require.Nil(t, startCmd.Flags().Lookup("update-interval"))
	require.Nil(t, startCmd.Flags().Lookup("max-price-age"))
	require.Nil(t, startCmd.Flags().Lookup("mode"))
	require.Nil(t, startCmd.Flags().Lookup("validation-period"))
}

func TestStartCmdUsesStartOptionsDefaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "oracle.json")
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
	configPath := filepath.Join(t.TempDir(), "oracle.json")
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

	logger.Info("oracle command logger labels")
	require.NoError(t, writeEnd.Close())
	output, err := io.ReadAll(readEnd)
	require.NoError(t, err)

	require.Contains(t, string(output), "service=oracle_sidecar")
}
