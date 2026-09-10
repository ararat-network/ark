package cmd

import (
	"bytes"
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ararat-network/ark/pricefeed/api"
)

func TestConfigCmdExposesValidateAndReload(t *testing.T) {
	configCmd, _, err := NewRootCmd().Find([]string{"config"})
	require.NoError(t, err)

	names := make([]string, 0, len(configCmd.Commands()))
	for _, sub := range configCmd.Commands() {
		names = append(names, sub.Name())
	}

	require.ElementsMatch(t, []string{"validate", "reload"}, names)
}

func TestConfigValidateAcceptsRuntimeConfig(t *testing.T) {
	cfgPath := writeConfig(t, validConfigTOML())
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--" + flagConfig, cfgPath, "config", "validate"})

	err := cmd.Execute()

	require.NoError(t, err)
	require.Equal(t, "pricefeed config is valid\n", out.String())
}

func TestConfigReloadCmdOwnsAdminAddressFlag(t *testing.T) {
	reloadCmd, _, err := NewRootCmd().Find([]string{"config", "reload"})
	require.NoError(t, err)

	flag := reloadCmd.Flags().Lookup(flagAdminAddress)

	require.NotNil(t, flag)
	require.Equal(t, defaultAdminAddress, flag.DefValue)
}

func TestReloadRuntimeConfigCallsPriceFeedAdminEndpoint(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	adminServer := &recordingPriceFeedAdminServer{}
	api.RegisterPriceFeedAdminServer(server, adminServer)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	err := reloadRuntimeConfig(
		context.Background(),
		"passthrough:///localhost:20196",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)

	require.NoError(t, err)
	require.True(t, adminServer.called)
}

func TestReloadRuntimeConfigRefusesOffLoopbackAdminAddress(t *testing.T) {
	err := reloadRuntimeConfig(context.Background(), "10.0.0.5:8081")

	require.ErrorContains(t, err, `admin address "10.0.0.5:8081" must be loopback`)
}

type recordingPriceFeedAdminServer struct {
	api.UnimplementedPriceFeedAdminServer

	called bool
}

func (s *recordingPriceFeedAdminServer) ReloadConfig(
	context.Context,
	*api.ReloadConfigRequest,
) (*api.ReloadConfigResponse, error) {
	s.called = true
	return &api.ReloadConfigResponse{}, nil
}
