package sidecar

import (
	"context"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/api"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// These tests exercise wrappers that bind configured addresses, complementing injected-listener
// serve tests. Incorrect binding or swallowed bind errors must not appear as successful startup.

// freeLoopbackAddress returns a loopback address nothing is listening on, by
// binding one and releasing it. A wrapper under test has to do its own
// binding, so the address cannot simply be handed over as a listener.
func freeLoopbackAddress(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := ln.Addr().String()
	require.NoError(t, ln.Close())

	return address
}

// heldLoopbackAddress returns an address held for the test's lifetime, so a
// wrapper asked to bind it fails.
func heldLoopbackAddress(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	return ln.Addr().String()
}

// requireGRPCAnswers retries until serving begins or the deadline expires. Any application
// response, including an error, proves the bound transport answers.
func requireGRPCAnswers(t *testing.T, address string, call func(context.Context, *grpc.ClientConn) error) {
	t.Helper()

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = call(ctx, conn)
		cancel()
		if code := status.Code(err); code != codes.Unavailable && code != codes.DeadlineExceeded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server at %s did not answer: %v", address, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServiceRunServesItsConfiguredAddress(t *testing.T) {
	address := freeLoopbackAddress(t)
	cfg := newTestRuntimeConfig()
	oracle := newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(sidecartypes.Prices{"NOAH/USD": big.NewFloat(1.5)}),
		newStaticChainStateClient(cfg.FallbackFeeds),
		ProcessConfig{ServerAddress: address},
	)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Run(ctx)
	}()

	requireGRPCAnswers(t, address, func(ctx context.Context, conn *grpc.ClientConn) error {
		_, err := api.NewPriceFeedClient(conn).Version(ctx, &api.VersionRequest{})

		return err
	})

	cancel()
	requireOracleStopped(t, errCh)
}

func TestServiceRunReturnsListenError(t *testing.T) {
	cfg := newTestRuntimeConfig()
	oracle := newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(nil),
		newStaticChainStateClient(cfg.FallbackFeeds),
		ProcessConfig{ServerAddress: heldLoopbackAddress(t)},
	)

	err := oracle.Run(context.Background())

	require.ErrorContains(t, err, "listen oracle server")
}

func TestServiceRunRejectsNilContext(t *testing.T) {
	oracle := newTestOracle(t, nil)

	//nolint:staticcheck // the nil context is the input under test.
	err := oracle.Run(nil)

	require.ErrorContains(t, err, "context cannot be nil")
}

func TestAdminServerRunServesItsConfiguredAddress(t *testing.T) {
	address := freeLoopbackAddress(t)
	oracle := newTestOracle(t, nil)
	admin, err := newAdminServer(oracle, log.NewNopLogger(), address)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- admin.run(ctx)
	}()

	requireGRPCAnswers(t, address, func(ctx context.Context, conn *grpc.ClientConn) error {
		// ReloadConfig fails on a service with no runtime config path. The
		// refusal is the proof: it came from the handler, so the transport
		// is up.
		_, err := api.NewPriceFeedAdminClient(conn).ReloadConfig(ctx, &api.ReloadConfigRequest{})

		return err
	})

	cancel()
	requireOracleStopped(t, errCh)
}

func TestAdminServerRunReturnsListenError(t *testing.T) {
	oracle := newTestOracle(t, nil)
	admin, err := newAdminServer(oracle, log.NewNopLogger(), heldLoopbackAddress(t))
	require.NoError(t, err)

	err = admin.run(context.Background())

	require.ErrorContains(t, err, "listen oracle admin server")
}

func TestAdminServerRunRejectsNilContext(t *testing.T) {
	oracle := newTestOracle(t, nil)
	admin, err := newAdminServer(oracle, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)

	//nolint:staticcheck // the nil context is the input under test.
	err = admin.run(nil)

	require.ErrorContains(t, err, "context cannot be nil")
}
