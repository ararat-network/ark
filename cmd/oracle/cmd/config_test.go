package cmd

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	transporttypes "ark/oracle/types"
)

func TestConfigValidateAcceptsRuntimeConfig(t *testing.T) {
	cfgPath := writeOracleConfig(t, validOracleConfigJSON())
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--" + flagConfig, cfgPath, "config", "validate"})

	err := cmd.Execute()

	require.NoError(t, err)
	require.Equal(t, "oracle config is valid\n", out.String())
}

func TestConfigReloadCommandOwnsAdminAddressFlag(t *testing.T) {
	cmd := NewRootCmd()
	configCmd, _, err := cmd.Find([]string{"config"})
	require.NoError(t, err)
	require.Nil(t, configCmd.Flags().Lookup(flagAdminAddress))

	reloadCmd, _, err := cmd.Find([]string{"config", "reload"})
	require.NoError(t, err)
	require.NotNil(t, reloadCmd.Flags().Lookup(flagAdminAddress))
	require.Nil(t, reloadCmd.Flags().Lookup(flagAddress))
	require.Equal(t, defaultAdminAddress, reloadCmd.Flags().Lookup(flagAdminAddress).DefValue)
}

func TestConfigShowCommandIsNotAvailable(t *testing.T) {
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"config", "show"})

	err := cmd.Execute()

	require.ErrorContains(t, err, "unknown command")
}

func TestReloadRuntimeConfigCallsOracleAdminEndpoint(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	adminServer := &recordingOracleAdminServer{}
	transporttypes.RegisterOracleAdminServer(server, adminServer)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	err := reloadRuntimeConfig(
		context.Background(),
		"passthrough:///oracle-admin",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)

	require.NoError(t, err)
	require.True(t, adminServer.called)
}

func TestConfigUpdateCommandIsNotAvailable(t *testing.T) {
	cfgPath := writeOracleConfig(t, validOracleConfigJSON())
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--" + flagConfig,
		cfgPath,
		"config",
		"update",
	})

	err := cmd.Execute()

	require.ErrorContains(t, err, "unknown command")
}

func validOracleConfigJSON() string {
	return `{
		"updateInterval": "1500ms",
		"providers": {
			"frankfurter_api": {
				"name": "frankfurter_api",
				"transportType": "api",
				"maxPriceAge": "90s",
				"markets": [
					{"pair": "NOAH/USD", "symbol": "NOAHUSD"}
				],
				"api": {
					"name": "frankfurter_api",
					"timeout": "3s",
					"interval": "1m",
					"endpoints": [{"url": "https://api.frankfurter.dev/v2/rates"}],
					"batchSize": 1
				}
			}
		},
		"resolver": {},
		"client": {
			"address": "127.0.0.1:9090",
			"timeout": "2s",
			"interval": "5s"
		},
		"fallbackFeeds": ["ausd"]
	}`
}

func writeOracleConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "oracle.json")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

type recordingOracleAdminServer struct {
	transporttypes.UnimplementedOracleAdminServer

	called bool
}

func (s *recordingOracleAdminServer) ReloadConfig(
	context.Context,
	*transporttypes.OracleReloadConfigRequest,
) (*transporttypes.OracleReloadConfigResponse, error) {
	s.called = true
	return &transporttypes.OracleReloadConfigResponse{}, nil
}
