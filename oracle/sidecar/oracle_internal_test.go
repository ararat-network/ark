package sidecar

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
)

func TestNewOracleDefaultsNilLogger(t *testing.T) {
	cfg := testInternalRuntimeConfig()

	oracle, err := NewOracle(Config{Runtime: cfg}, nil)

	require.NoError(t, err)
	require.NotNil(t, oracle)
}

func TestOracleAndServerDeriveComponentLoggers(t *testing.T) {
	logs := new(bytes.Buffer)
	rootLogger := log.NewLogger(logs)
	cfg := testInternalRuntimeConfig()

	oracle, err := NewOracle(Config{Runtime: cfg}, rootLogger)
	require.NoError(t, err)
	require.NotNil(t, oracle.server)

	oracle.logger.Info("oracle component logger")
	oracle.server.logger.Info("transport component logger")
	output := logs.String()

	require.Contains(t, output, "component=oracle")
	require.Contains(t, output, "component=transport")
	require.NotContains(t, output, "server=oracle")
}

func TestNewOraclePreparesServerTransport(t *testing.T) {
	cfg := testInternalRuntimeConfig()

	oracle, err := NewOracle(Config{Runtime: cfg}, nil)

	require.NoError(t, err)
	require.NotNil(t, oracle.server)
	require.NotNil(t, oracle.server.httpSrv)
	require.NotNil(t, oracle.server.grpcSrv)
	require.NotNil(t, oracle.server.gatewayMux)
}

func TestNewOracleAppliesProcessConfigToServer(t *testing.T) {
	runtimeCfg := testInternalRuntimeConfig()
	processCfg := ProcessConfig{
		ServerAddress: "127.0.0.1:18080",
	}

	oracle, err := NewOracle(Config{
		Runtime: runtimeCfg,
		Process: processCfg,
	}, nil)

	require.NoError(t, err)
	require.Equal(t, processCfg.ServerAddress, oracle.server.address)
}
