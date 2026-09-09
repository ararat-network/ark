package sidecar

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"
)

func TestNewServiceConfiguresSeparateLoopbackAdminServer(t *testing.T) {
	configPath := writeRuntimeConfig(t, testInternalRuntimeConfig())

	oracle, err := NewService(Config{
		Runtime: testInternalRuntimeConfig(),
		Process: ProcessConfig{
			AdminAddress:      "127.0.0.1:18081",
			RuntimeConfigPath: configPath,
		},
	}, nil)

	require.NoError(t, err)
	require.Equal(t, configPath, oracle.runtimeConfigPath)
	require.Equal(t, "127.0.0.1:18081", oracle.adminServer.address)
	require.Contains(t, oracle.server.grpcSrv.GetServiceInfo(), "ark.pricefeed.v1.PriceFeed")
	require.NotContains(t, oracle.server.grpcSrv.GetServiceInfo(), "ark.pricefeed.v1.PriceFeedAdmin")
	require.Contains(t, oracle.adminServer.grpcSrv.GetServiceInfo(), "ark.pricefeed.v1.PriceFeedAdmin")
	require.NotContains(t, oracle.adminServer.grpcSrv.GetServiceInfo(), "ark.pricefeed.v1.PriceFeed")
}

func TestNewServiceRejectsUnsafeAdminConfig(t *testing.T) {
	testCases := []struct {
		name       string
		processCfg ProcessConfig
		errMessage string
	}{
		{
			name: "invalid address",
			processCfg: ProcessConfig{
				AdminAddress:      "127.0.0.1",
				RuntimeConfigPath: "oracle.toml",
			},
			errMessage: "oracle admin server address",
		},
		{
			name: "missing config path",
			processCfg: ProcessConfig{
				AdminAddress: "127.0.0.1:18081",
			},
			errMessage: "runtime config path is required",
		},
		{
			name: "non-loopback host",
			processCfg: ProcessConfig{
				AdminAddress:      "0.0.0.0:18081",
				RuntimeConfigPath: "oracle.toml",
			},
			errMessage: "must be a loopback IP address",
		},
		{
			name: "hostname is not accepted",
			processCfg: ProcessConfig{
				AdminAddress:      "localhost:18081",
				RuntimeConfigPath: "oracle.toml",
			},
			errMessage: "must be a loopback IP address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			oracle, err := NewService(Config{
				Runtime: testInternalRuntimeConfig(),
				Process: tc.processCfg,
			}, nil)

			require.Nil(t, oracle)
			require.ErrorContains(t, err, tc.errMessage)
		})
	}
}

func TestNewAdminServerNormalisesBracketedIPv6Loopback(t *testing.T) {
	oracle, err := NewService(Config{Runtime: testInternalRuntimeConfig()}, nil)
	require.NoError(t, err)

	admin, err := newAdminServer(oracle, log.NewNopLogger(), "[::1]:0")

	require.NoError(t, err)
	require.Equal(t, "[::1]:0", admin.address)
}

func TestAdminServerServeStopsOnContextCancellation(t *testing.T) {
	oracle, err := NewService(Config{Runtime: testInternalRuntimeConfig()}, nil)
	require.NoError(t, err)
	admin, err := newAdminServer(oracle, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)
	listener := newBlockingListener()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- admin.serve(ctx, listener)
	}()
	select {
	case <-listener.accepted:
	case <-time.After(time.Second):
		t.Fatal("admin server did not start accepting connections")
	}

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("admin server did not stop")
	}
}

func TestAdminServerServeReturnsListenerError(t *testing.T) {
	oracle, err := NewService(Config{Runtime: testInternalRuntimeConfig()}, nil)
	require.NoError(t, err)
	admin, err := newAdminServer(oracle, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)
	listener := bufconn.Listen(1024 * 1024)
	require.NoError(t, listener.Close())

	err = admin.serve(context.Background(), listener)

	require.ErrorContains(t, err, "serve oracle admin requests")
}
