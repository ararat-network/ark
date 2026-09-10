package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartCmdUsesStartOptionsDefaults(t *testing.T) {
	options := startOptions{
		address:        defaultAddress,
		adminAddress:   defaultAdminAddress,
		metrics:        defaultMetrics,
		metricsAddress: defaultMetricsAddress,
		pprof:          defaultPprof,
		pprofAddress:   defaultPprofAddress,
	}

	startCmd := newStartCmd(&rootOptions{})
	flags := startCmd.Flags()

	require.Equal(t, options.address, flags.Lookup(flagAddress).DefValue)
	require.Equal(t, options.adminAddress, flags.Lookup(flagAdminAddress).DefValue)
	require.Equal(t, "false", flags.Lookup(flagMetrics).DefValue)
	require.Equal(t, options.metricsAddress, flags.Lookup(flagMetricsAddress).DefValue)
	require.Equal(t, "false", flags.Lookup(flagPprof).DefValue)
	require.Equal(t, options.pprofAddress, flags.Lookup(flagPprofAddress).DefValue)
}

func TestStartCmdBooleanFlagsEnableByPresence(t *testing.T) {
	flags := newStartCmd(&rootOptions{}).Flags()

	require.NoError(t, flags.Parse([]string{"--" + flagMetrics, "--" + flagPprof}))
	metrics, err := flags.GetBool(flagMetrics)
	require.NoError(t, err)
	pprof, err := flags.GetBool(flagPprof)
	require.NoError(t, err)

	require.True(t, metrics)
	require.True(t, pprof)
}
