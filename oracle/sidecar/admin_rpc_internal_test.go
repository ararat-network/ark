package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"

	"ark/oracle/sidecar/chainstate"
	"ark/oracle/sidecar/runtime"
	transporttypes "ark/oracle/types"
)

func TestOracleReloadConfigLoadsConstructionPath(t *testing.T) {
	initialCfg := newTestRuntimeConfig()
	reloadedCfg := initialCfg.Clone()
	reloadedCfg.FallbackDenoms = []string{"ueur"}
	configPath := writeRuntimeConfig(t, reloadedCfg)
	oracle := newReloadTestOracle(t, initialCfg, configPath)
	startTestRuntime(t, oracle)
	initial := requireOracleTick(t, oracle)
	require.Contains(t, initial.Prices, "uusd")

	err := oracle.ReloadConfig(context.Background())

	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot := oracle.runtime.GetPriceSnapshot()
		_, hasEUR := snapshot.Prices["ueur"]
		_, hasUSD := snapshot.Prices["uusd"]
		return snapshot.Timestamp.After(initial.Timestamp) && hasEUR && !hasUSD
	}, time.Second, time.Millisecond)
}

func TestOracleReloadConfigPreservesRuntimeAfterInvalidFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "oracle.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"updateInterval":"0s"}`), 0o600))
	oracle := newReloadTestOracle(t, newTestRuntimeConfig(), configPath)
	startTestRuntime(t, oracle)
	initial := requireOracleTick(t, oracle)

	err := oracle.ReloadConfig(context.Background())

	require.ErrorContains(t, err, "oracle update interval must be greater than 0")
	snapshot := oracle.runtime.GetPriceSnapshot()
	require.Equal(t, initial.Timestamp, snapshot.Timestamp)
	require.Contains(t, snapshot.Prices, "uusd")
}

func TestAdminServiceReloadsConfigOverGRPC(t *testing.T) {
	initialCfg := newTestRuntimeConfig()
	reloadedCfg := initialCfg.Clone()
	reloadedCfg.FallbackDenoms = []string{"ueur"}
	configPath := writeRuntimeConfig(t, reloadedCfg)
	oracle := newReloadTestOracle(t, initialCfg, configPath)
	startTestRuntime(t, oracle)
	initial := requireOracleTick(t, oracle)
	admin, err := newAdminServer(oracle, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)

	listener := bufconn.Listen(1024 * 1024)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- admin.serve(ctx, listener)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Error("admin server did not stop")
		}
	})

	conn, err := grpc.NewClient(
		"passthrough:///oracle-admin",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	client := transporttypes.NewOracleAdminClient(conn)
	_, err = client.ReloadConfig(context.Background(), &transporttypes.OracleReloadConfigRequest{})

	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot := oracle.runtime.GetPriceSnapshot()
		_, ok := snapshot.Prices["ueur"]
		return snapshot.Timestamp.After(initial.Timestamp) && ok
	}, time.Second, time.Millisecond)
}

func TestAdminServerPanicRecoveryReturnsInternalStatus(t *testing.T) {
	oracle, err := NewOracle(Config{Runtime: testInternalRuntimeConfig()}, nil)
	require.NoError(t, err)
	admin, err := newAdminServer(oracle, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)

	response, err := admin.recoverUnaryPanic(
		context.Background(),
		nil,
		nil,
		func(context.Context, any) (any, error) {
			panic("reload exploded")
		},
	)

	require.Nil(t, response)
	require.Equal(t, codes.Internal, status.Code(err))
}

func newReloadTestOracle(t *testing.T, cfg runtime.Config, configPath string) *Oracle {
	t.Helper()
	return newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(nil),
		unavailableVoteTargetsClient{},
		ProcessConfig{
			ServerAddress:     "127.0.0.1:0",
			RuntimeConfigPath: configPath,
		},
	)
}

type unavailableVoteTargetsClient struct{}

func (unavailableVoteTargetsClient) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (unavailableVoteTargetsClient) Update(chainstate.Config) {}

func (unavailableVoteTargetsClient) VoteTargets() ([]string, error) {
	return nil, errors.New("vote targets unavailable")
}

func writeRuntimeConfig(t *testing.T, cfg any) string {
	t.Helper()

	bz, err := json.Marshal(cfg)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "oracle.json")
	require.NoError(t, os.WriteFile(path, bz, 0o600))

	return path
}
