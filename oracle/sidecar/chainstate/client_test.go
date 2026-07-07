package chainstate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log/v2"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"noah/oracle/sidecar/chainstate"
	oracletypes "noah/x/oracle/types"
)

const bufSize = 1024 * 1024

func TestStartPollsImmediatelyAndCachesVoteTargets(t *testing.T) {
	source := []string{"uusd", "ukrw"}
	query := newFakeQueryServer(queryResult{targets: source})
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd", "ukrw"})

	source[0] = "umutated"
	got, err := client.VoteTargets()
	require.NoError(t, err)
	require.Equal(t, []string{"uusd", "ukrw"}, got)

	got[0] = "umodified"
	got, err = client.VoteTargets()
	require.NoError(t, err)
	require.Equal(t, []string{"uusd", "ukrw"}, got)
}

func TestStartReturnsAfterLaunchingPollingLoop(t *testing.T) {
	query := newFakeQueryServer(queryResult{targets: []string{"uusd"}})
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Start(ctx)
	}()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		client.Stop()
		cancel()
		select {
		case <-errCh:
		case <-time.After(time.Second):
		}
		t.Fatal("Start blocked after launching polling loop")
	}
	defer client.Stop()

	query.waitForCalls(t, 1)
}

func TestStartLogsLifecycleAndInitialVoteTargets(t *testing.T) {
	logs := &lockedBuffer{}
	query := newFakeQueryServer(queryResult{targets: []string{"uusd", "ukrw"}})
	endpoint := newTestQueryEndpoint(t, "bufnet", query)
	client, err := chainstate.NewClient(
		chainstate.Config{
			Address:  endpoint.address,
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		chainstate.WithDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(endpoint))),
		chainstate.WithLogger(log.NewLogger(logs)),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd", "ukrw"})
	stopClient(cancel, client)

	output := logs.String()
	require.Contains(t, output, "starting chain state vote-target client")
	require.Contains(t, output, "stopping chain state vote-target client")
	require.Contains(t, output, "chain state vote-target client stopped")
}

func TestVoteTargetsReturnsErrorBeforeFirstSuccessfulPoll(t *testing.T) {
	query := newFakeQueryServer(queryResult{err: errors.New("node unavailable")})
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	got, err := client.VoteTargets()
	require.ErrorContains(t, err, "no vote targets fetched yet")
	require.Nil(t, got)
}

func TestStartKeepsLastVoteTargetsAfterRefreshFailure(t *testing.T) {
	query := newFakeQueryServer(
		queryResult{targets: []string{"uusd"}},
		queryResult{err: errors.New("node unavailable")},
	)
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd"})
	query.waitForCalls(t, 2)

	got, err := client.VoteTargets()
	require.NoError(t, err)
	require.Equal(t, []string{"uusd"}, got)
}

func TestStartLogsRefreshFailureWhileKeepingLastVoteTargets(t *testing.T) {
	logs := &lockedBuffer{}
	query := newFakeQueryServer(
		queryResult{targets: []string{"uusd"}},
		queryResult{err: errors.New("node unavailable")},
		queryResult{targets: []string{"uusd", "ukrw"}},
	)
	endpoint := newTestQueryEndpoint(t, "bufnet", query)
	client, err := chainstate.NewClient(
		chainstate.Config{
			Address:  endpoint.address,
			Timeout:  time.Second,
			Interval: time.Millisecond,
		},
		chainstate.WithDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(endpoint))),
		chainstate.WithLogger(log.NewLogger(logs)),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd"})
	query.waitForCalls(t, 2)
	query.waitForCalls(t, 3)

	require.Eventually(t, func() bool {
		output := logs.String()
		return strings.Contains(output, "failed to refresh chain state vote targets") &&
			strings.Contains(output, "node unavailable")
	}, time.Second, time.Millisecond)

	got, err := client.VoteTargets()
	require.NoError(t, err)
	require.Equal(t, []string{"uusd", "ukrw"}, got)
}

func TestStartRecordsChainStateRefreshMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	query := newFakeQueryServer(
		queryResult{targets: []string{"uusd", "ukrw"}},
		queryResult{err: errors.New("node unavailable")},
	)
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	cancel := startClient(t, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd", "ukrw"})
	query.waitForCalls(t, 2)
	stopClient(cancel, client)

	require.Eventually(t, func() bool {
		families, err := registry.Gather()
		if err != nil {
			return false
		}

		refreshes := chainStateMetricFamily(families, "noah_oracle_chainstate_refreshes_total")
		if refreshes == nil {
			return false
		}
		return chainStateCounterValue(refreshes, map[string]string{"status": "success"}) >= 1 &&
			chainStateCounterValue(refreshes, map[string]string{"status": "error"}) >= 1
	}, time.Second, time.Millisecond)
}

func TestStartRejectsInvalidRefreshWithoutClearingCache(t *testing.T) {
	query := newFakeQueryServer(
		queryResult{targets: []string{"uusd"}},
		queryResult{targets: []string{"uusd", "uusd"}},
	)
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd"})
	query.waitForCalls(t, 2)

	got, err := client.VoteTargets()
	require.NoError(t, err)
	require.Equal(t, []string{"uusd"}, got)
}

func TestStopCancelsStart(t *testing.T) {
	query := newFakeQueryServer(queryResult{targets: []string{"uusd"}})
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	defer cancel()

	query.waitForCalls(t, 1)
	client.Stop()

	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	require.NoError(t, client.Start(restartCtx))
	client.Stop()
}

func TestUpdateConfigAppliesIntervalChange(t *testing.T) {
	query := newFakeQueryServer(queryResult{targets: []string{"uusd"}})
	client := newTestClient(t, query, chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	client.Update(chainstate.Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	query.waitForCalls(t, 2)
}

func TestUpdateConfigReconnectsWhenAddressChanges(t *testing.T) {
	firstQuery := newFakeQueryServer(queryResult{targets: []string{"uusd"}})
	secondQuery := newFakeQueryServer(queryResult{targets: []string{"ukrw"}})
	firstEndpoint := newTestQueryEndpoint(t, "first", firstQuery)
	secondEndpoint := newTestQueryEndpoint(t, "second", secondQuery)

	client, err := chainstate.NewClient(
		chainstate.Config{
			Address:  firstEndpoint.address,
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		chainstate.WithDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(firstEndpoint, secondEndpoint))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	firstQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd"})

	client.Update(chainstate.Config{
		Address:  secondEndpoint.address,
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	secondQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ukrw"})
}

func TestUpdateConfigLogsConfigChangeAndReconnect(t *testing.T) {
	logs := &lockedBuffer{}
	firstQuery := newFakeQueryServer(queryResult{targets: []string{"uusd"}})
	secondQuery := newFakeQueryServer(queryResult{targets: []string{"ukrw"}})
	firstEndpoint := newTestQueryEndpoint(t, "first", firstQuery)
	secondEndpoint := newTestQueryEndpoint(t, "second", secondQuery)

	client, err := chainstate.NewClient(
		chainstate.Config{
			Address:  firstEndpoint.address,
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		chainstate.WithDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(firstEndpoint, secondEndpoint))),
		chainstate.WithLogger(log.NewLogger(logs)),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	firstQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"uusd"})

	client.Update(chainstate.Config{
		Address:  secondEndpoint.address,
		Timeout:  2 * time.Second,
		Interval: time.Millisecond,
	})

	secondQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ukrw"})

	require.Eventually(t, func() bool {
		output := logs.String()
		return strings.Contains(output, "updated chain state vote-target client config") &&
			strings.Contains(output, "reconnecting chain state vote-target client after address update")
	}, time.Second, time.Millisecond)
}

func TestUpdateConfigAllowsStaleVoteTargetsFromPreviousAddressUntilNextPoll(t *testing.T) {
	firstQuery := newBlockingQueryServer(queryResult{targets: []string{"uusd"}})
	secondQuery := newBlockingQueryServer(queryResult{targets: []string{"ukrw"}})
	firstEndpoint := newTestQueryEndpoint(t, "first", firstQuery)
	secondEndpoint := newTestQueryEndpoint(t, "second", secondQuery)

	client, err := chainstate.NewClient(
		chainstate.Config{
			Address:  firstEndpoint.address,
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		chainstate.WithDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(firstEndpoint, secondEndpoint))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	firstQuery.waitForCalls(t, 1)
	client.Update(chainstate.Config{
		Address:  secondEndpoint.address,
		Timeout:  time.Second,
		Interval: time.Hour,
	})
	firstQuery.release()

	secondQuery.waitForCalls(t, 1)
	got, err := client.VoteTargets()
	require.NoError(t, err)
	require.Equal(t, []string{"uusd"}, got)

	secondQuery.release()
	requireEventuallyTargets(t, client, []string{"ukrw"})
}

func TestConfigValidateRejectsInvalidConfig(t *testing.T) {
	cfg := chainstate.Config{
		Address:  "",
		Timeout:  time.Second,
		Interval: time.Second,
	}
	err := cfg.Validate()

	require.ErrorContains(t, err, "address")
}

func TestNewClientRejectsMissingAddress(t *testing.T) {
	client, err := chainstate.NewClient(chainstate.Config{
		Timeout:  time.Second,
		Interval: time.Second,
	})
	require.Nil(t, client)
	require.ErrorContains(t, err, "address")
}

type queryResult struct {
	targets []string
	err     error
}

type blockingQueryServer struct {
	oracletypes.UnimplementedQueryServer

	result    queryResult
	calls     chan struct{}
	releaseCh chan struct{}
	once      sync.Once
}

func newBlockingQueryServer(result queryResult) *blockingQueryServer {
	return &blockingQueryServer{
		result:    result,
		calls:     make(chan struct{}, 100),
		releaseCh: make(chan struct{}),
	}
}

func (b *blockingQueryServer) VoteTargets(
	ctx context.Context,
	_ *oracletypes.QueryVoteTargetsRequest,
) (*oracletypes.QueryVoteTargetsResponse, error) {
	b.calls <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.releaseCh:
	}

	if b.result.err != nil {
		return nil, b.result.err
	}
	return &oracletypes.QueryVoteTargetsResponse{VoteTargets: b.result.targets}, nil
}

func (b *blockingQueryServer) waitForCalls(t *testing.T, want int) {
	t.Helper()
	for range want {
		select {
		case <-b.calls:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for vote-target query call %d", want)
		}
	}
}

func (b *blockingQueryServer) release() {
	b.once.Do(func() {
		close(b.releaseCh)
	})
}

type fakeQueryServer struct {
	oracletypes.UnimplementedQueryServer

	mut     sync.Mutex
	results []queryResult
	calls   chan struct{}
}

func newFakeQueryServer(results ...queryResult) *fakeQueryServer {
	return &fakeQueryServer{
		results: results,
		calls:   make(chan struct{}, 100),
	}
}

func (f *fakeQueryServer) VoteTargets(
	_ context.Context,
	_ *oracletypes.QueryVoteTargetsRequest,
) (*oracletypes.QueryVoteTargetsResponse, error) {
	f.mut.Lock()
	defer f.mut.Unlock()

	result := f.results[len(f.results)-1]
	if len(f.results) > 1 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	f.calls <- struct{}{}

	if result.err != nil {
		return nil, result.err
	}

	return &oracletypes.QueryVoteTargetsResponse{VoteTargets: result.targets}, nil
}

func (f *fakeQueryServer) waitForCalls(t *testing.T, want int) {
	t.Helper()
	for range want {
		select {
		case <-f.calls:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for vote-target query call %d", want)
		}
	}
}

func newTestClient(t *testing.T, query oracletypes.QueryServer, cfg chainstate.Config) *chainstate.Client {
	t.Helper()

	endpoint := newTestQueryEndpoint(t, "bufnet", query)

	client, err := chainstate.NewClient(
		cfg,
		chainstate.WithDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(endpoint))),
	)
	require.NoError(t, err)

	return client
}

type testQueryEndpoint struct {
	address  string
	target   string
	listener *bufconn.Listener
}

func newTestQueryEndpoint(t *testing.T, target string, query oracletypes.QueryServer) testQueryEndpoint {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	oracletypes.RegisterQueryServer(server, query)
	go func() {
		_ = server.Serve(listener)
	}()

	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	return testQueryEndpoint{
		address:  "passthrough:///" + target,
		target:   target,
		listener: listener,
	}
}

func dialTestQueryEndpoints(endpoints ...testQueryEndpoint) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, target string) (net.Conn, error) {
		target = strings.TrimPrefix(target, "passthrough:///")
		for _, endpoint := range endpoints {
			if target == endpoint.target || target == endpoint.address {
				return endpoint.listener.DialContext(ctx)
			}
		}
		return nil, fmt.Errorf("unexpected dial target %q", target)
	}
}

type pollingClient interface {
	Start(context.Context) error
	Stop()
	VoteTargets() ([]string, error)
}

func startClient(t *testing.T, client pollingClient) context.CancelFunc {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, client.Start(ctx))

	return cancel
}

func stopClient(cancel context.CancelFunc, client pollingClient) {
	cancel()
	client.Stop()
}

func requireEventuallyTargets(
	t *testing.T,
	client interface {
		VoteTargets() ([]string, error)
	},
	want []string,
) {
	t.Helper()

	require.Eventually(t, func() bool {
		got, err := client.VoteTargets()
		return err == nil && reflect.DeepEqual(want, got)
	}, time.Second, time.Millisecond)
}

func chainStateMetricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	return nil
}

func chainStateCounterValue(family *dto.MetricFamily, labels map[string]string) float64 {
	metric := chainStateMatchingMetric(family, labels)
	if metric == nil {
		return 0
	}

	return metric.GetCounter().GetValue()
}

func chainStateMatchingMetric(family *dto.MetricFamily, labels map[string]string) *dto.Metric {
	for _, metric := range family.Metric {
		actual := make(map[string]string, len(metric.Label))
		for _, label := range metric.Label {
			actual[label.GetName()] = label.GetValue()
		}
		matches := true
		for name, value := range labels {
			if actual[name] != value {
				matches = false
				break
			}
		}
		if matches {
			return metric
		}
	}

	return nil
}

type lockedBuffer struct {
	mut sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mut.Lock()
	defer b.mut.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mut.Lock()
	defer b.mut.Unlock()

	return b.buf.String()
}
