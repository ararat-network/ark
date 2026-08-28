package chainstate

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

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"

	oracletypes "ark/x/oracle/types"
)

const bufSize = 1024 * 1024

func TestRunPollsImmediatelyAndCachesFeeds(t *testing.T) {
	source := []string{"akrw", "ausd"}
	query := newFakeQueryServer(feedResult(source))
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"akrw", "ausd"})

	source[0] = "amutated"
	got, err := client.Feeds()
	require.NoError(t, err)
	require.Equal(t, []string{"akrw", "ausd"}, got)

	got[0] = "amodified"
	got, err = client.Feeds()
	require.NoError(t, err)
	require.Equal(t, []string{"akrw", "ausd"}, got)
}

func TestRunCachesScheduledAdditionsForProviderWarmup(t *testing.T) {
	query := newFakeQueryServer(queryResult{
		feeds:   []string{"ausd"},
		version: oracletypes.InitialFeedVersion,
		transitions: []oracletypes.FeedTransition{
			{
				Denom:                "aaud",
				Direction:            oracletypes.FeedDirection_FEED_DIRECTION_ADD,
				ActivationVoteHeight: 10,
			},
		},
	})
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"aaud", "ausd"})
}

func TestRunBlocksUntilContextCancellation(t *testing.T) {
	query := newFakeQueryServer(feedResult([]string{"ausd"}))
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx)
	}()

	query.waitForCalls(t, 1)

	select {
	case err := <-errCh:
		require.NoError(t, err)
		t.Fatal("Run returned before context cancellation")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRunLogsLifecycleAndInitialFeeds(t *testing.T) {
	logs := &lockedBuffer{}
	query := newFakeQueryServer(feedResult([]string{"akrw", "ausd"}))
	endpoint := newTestQueryEndpoint(t, "bufnet", query)
	client, err := NewClient(
		Config{
			Address:  endpoint.address,
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		withDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(endpoint))),
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"akrw", "ausd"})
	stopClient(cancel, client)

	output := logs.String()
	require.Contains(t, output, "starting chain state client")
}

func TestFeedsReturnsErrorBeforeFirstSuccessfulPoll(t *testing.T) {
	logs := &lockedBuffer{}
	query := newFakeQueryServer(queryResult{err: errors.New("node unavailable")})
	endpoint := newTestQueryEndpoint(t, "bufnet", query)
	client, err := NewClient(
		Config{
			Address:  endpoint.address,
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		withDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(endpoint))),
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), "failed to refresh chain state feeds")
	}, time.Second, time.Millisecond)

	got, err := client.Feeds()
	require.EqualError(t, err, "no feeds fetched yet")
	require.Nil(t, got)
}

func TestRunCachesAuthoritativeEmptyFeeds(t *testing.T) {
	query := newFakeQueryServer(feedResult([]string{}))
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{})
}

func TestRunReplacesNonEmptyFeedsWithEmptySnapshot(t *testing.T) {
	query := newFakeQueryServer(
		feedResult([]string{"ausd"}),
		feedResult([]string{}),
		queryResult{err: errors.New("node unavailable")},
	)
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	query.waitForCalls(t, 2)
	requireEventuallyTargets(t, client, []string{})
	query.waitForCalls(t, 3)

	got, err := client.Feeds()
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRunKeepsLastFeedsAfterRefreshFailure(t *testing.T) {
	query := newFakeQueryServer(
		feedResult([]string{"ausd"}),
		queryResult{err: errors.New("node unavailable")},
	)
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ausd"})
	query.waitForCalls(t, 2)

	got, err := client.Feeds()
	require.NoError(t, err)
	require.Equal(t, []string{"ausd"}, got)
}

func TestRunLogsRefreshFailureWhileKeepingLastFeeds(t *testing.T) {
	logs := &lockedBuffer{}
	query := newFakeQueryServer(
		feedResult([]string{"ausd"}),
		queryResult{err: errors.New("node unavailable")},
		feedResult([]string{"akrw", "ausd"}),
	)
	endpoint := newTestQueryEndpoint(t, "bufnet", query)
	client, err := NewClient(
		Config{
			Address:  endpoint.address,
			Timeout:  time.Second,
			Interval: time.Millisecond,
		},
		withDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(endpoint))),
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ausd"})
	query.waitForCalls(t, 2)
	query.waitForCalls(t, 3)

	require.Eventually(t, func() bool {
		output := logs.String()
		return strings.Contains(output, "failed to refresh chain state feeds") &&
			strings.Contains(output, "node unavailable")
	}, time.Second, time.Millisecond)

	got, err := client.Feeds()
	require.NoError(t, err)
	require.Equal(t, []string{"akrw", "ausd"}, got)
}

func TestRunRecordsChainStateRefreshMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	query := newFakeQueryServer(
		feedResult([]string{"akrw", "ausd"}),
		queryResult{err: errors.New("node unavailable")},
	)
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	cancel := startClient(t, client)

	query.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"akrw", "ausd"})
	query.waitForCalls(t, 2)
	stopClient(cancel, client)

	require.Eventually(t, func() bool {
		families, err := registry.Gather()
		if err != nil {
			return false
		}

		refreshes := chainStateMetricFamily(families, "ark_pricefeed_chainstate_refreshes_total")
		if refreshes == nil {
			return false
		}
		return chainStateCounterValue(refreshes, map[string]string{"status": "success"}) >= 1 &&
			chainStateCounterValue(refreshes, map[string]string{"status": "error"}) >= 1
	}, time.Second, time.Millisecond)
}

func TestRunCanRunAgainAfterContextCancellation(t *testing.T) {
	query := newFakeQueryServer(feedResult([]string{"ausd"}))
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Hour,
	})

	cancel := startClient(t, client)
	query.waitForCalls(t, 1)
	cancel()

	restartCleanup := startClient(t, client)
	query.waitForCalls(t, 1)
	restartCleanup()
}

func TestUpdateConfigAppliesIntervalChangeAfterNextTick(t *testing.T) {
	query := newFakeQueryServer(feedResult([]string{"ausd"}))
	originalInterval := 75 * time.Millisecond
	client := newTestClient(t, query, Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: originalInterval,
	})

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	query.waitForCalls(t, 1)
	client.Update(Config{
		Address:  "passthrough:///bufnet",
		Timeout:  time.Second,
		Interval: time.Millisecond,
	})

	query.requireNoCalls(t, originalInterval/3)
	query.waitForCalls(t, 1)
	query.waitForCalls(t, 1)
}

func TestUpdateConfigReconnectsWhenAddressChangesAfterNextTick(t *testing.T) {
	firstQuery := newFakeQueryServer(feedResult([]string{"ausd"}))
	secondQuery := newFakeQueryServer(feedResult([]string{"akrw"}))
	firstEndpoint := newTestQueryEndpoint(t, "first", firstQuery)
	secondEndpoint := newTestQueryEndpoint(t, "second", secondQuery)
	originalInterval := 75 * time.Millisecond

	client, err := NewClient(
		Config{
			Address:  firstEndpoint.address,
			Timeout:  time.Second,
			Interval: originalInterval,
		},
		withDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(firstEndpoint, secondEndpoint))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	firstQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ausd"})

	client.Update(Config{
		Address:  secondEndpoint.address,
		Timeout:  time.Second,
		Interval: originalInterval,
	})

	secondQuery.requireNoCalls(t, originalInterval/3)
	secondQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"akrw"})
}

func TestUpdateConfigLogsConfigChangeAndReconnect(t *testing.T) {
	logs := &lockedBuffer{}
	firstQuery := newFakeQueryServer(feedResult([]string{"ausd"}))
	secondQuery := newFakeQueryServer(feedResult([]string{"akrw"}))
	firstEndpoint := newTestQueryEndpoint(t, "first", firstQuery)
	secondEndpoint := newTestQueryEndpoint(t, "second", secondQuery)

	client, err := NewClient(
		Config{
			Address:  firstEndpoint.address,
			Timeout:  time.Second,
			Interval: 75 * time.Millisecond,
		},
		withDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(firstEndpoint, secondEndpoint))),
		WithLogger(log.NewLogger(logs, log.ColorOption(false))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	firstQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"ausd"})

	client.Update(Config{
		Address:  secondEndpoint.address,
		Timeout:  2 * time.Second,
		Interval: time.Millisecond,
	})

	secondQuery.waitForCalls(t, 1)
	requireEventuallyTargets(t, client, []string{"akrw"})

	require.Eventually(t, func() bool {
		output := logs.String()
		return strings.Contains(output, "updated chain state feed client config") &&
			strings.Contains(output, "reconnecting chain state feed client after address update")
	}, time.Second, time.Millisecond)
}

func TestUpdateConfigAllowsStaleFeedsFromPreviousAddressUntilNextPoll(t *testing.T) {
	firstQuery := newBlockingQueryServer(feedResult([]string{"ausd"}))
	secondQuery := newBlockingQueryServer(feedResult([]string{"akrw"}))
	firstEndpoint := newTestQueryEndpoint(t, "first", firstQuery)
	secondEndpoint := newTestQueryEndpoint(t, "second", secondQuery)
	originalInterval := 75 * time.Millisecond

	client, err := NewClient(
		Config{
			Address:  firstEndpoint.address,
			Timeout:  time.Second,
			Interval: originalInterval,
		},
		withDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(firstEndpoint, secondEndpoint))),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	firstQuery.waitForCalls(t, 1)
	client.Update(Config{
		Address:  secondEndpoint.address,
		Timeout:  time.Second,
		Interval: originalInterval,
	})
	firstQuery.release()

	requireEventuallyTargets(t, client, []string{"ausd"})

	secondQuery.requireNoCalls(t, originalInterval/3)
	secondQuery.waitForCalls(t, 1)
	got, err := client.Feeds()
	require.NoError(t, err)
	require.Equal(t, []string{"ausd"}, got)

	secondQuery.release()
	requireEventuallyTargets(t, client, []string{"akrw"})
}

func TestConfigValidateRejectsInvalidConfig(t *testing.T) {
	cfg := Config{
		Address:  "",
		Timeout:  time.Second,
		Interval: time.Second,
	}
	err := cfg.Validate()

	require.ErrorContains(t, err, "address")
}

func TestNewClientRejectsMissingAddress(t *testing.T) {
	client, err := NewClient(Config{
		Timeout:  time.Second,
		Interval: time.Second,
	})
	require.Nil(t, client)
	require.ErrorContains(t, err, "address")
}

type queryResult struct {
	feeds       []string
	version     uint64
	transitions []oracletypes.FeedTransition
	err         error
}

func feedResult(feeds []string) queryResult {
	return queryResult{
		feeds:   feeds,
		version: oracletypes.InitialFeedVersion,
	}
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

func (b *blockingQueryServer) Feeds(
	ctx context.Context,
	_ *oracletypes.QueryFeedsRequest,
) (*oracletypes.QueryFeedsResponse, error) {
	b.calls <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.releaseCh:
	}

	if b.result.err != nil {
		return nil, b.result.err
	}
	return &oracletypes.QueryFeedsResponse{
		Feeds: oracletypes.Feeds{
			Denoms:      b.result.feeds,
			Version:     b.result.version,
			Transitions: b.result.transitions,
		},
	}, nil
}

func (b *blockingQueryServer) waitForCalls(t *testing.T, want int) {
	t.Helper()
	for range want {
		select {
		case <-b.calls:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for feed query call %d", want)
		}
	}
}

func (b *blockingQueryServer) requireNoCalls(t *testing.T, duration time.Duration) {
	t.Helper()
	select {
	case <-b.calls:
		t.Fatal("unexpected feed query call")
	case <-time.After(duration):
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

func (f *fakeQueryServer) Feeds(
	_ context.Context,
	_ *oracletypes.QueryFeedsRequest,
) (*oracletypes.QueryFeedsResponse, error) {
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

	return &oracletypes.QueryFeedsResponse{
		Feeds: oracletypes.Feeds{
			Denoms:      result.feeds,
			Version:     result.version,
			Transitions: result.transitions,
		},
	}, nil
}

func (f *fakeQueryServer) waitForCalls(t *testing.T, want int) {
	t.Helper()
	for range want {
		select {
		case <-f.calls:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for feed query call %d", want)
		}
	}
}

func (f *fakeQueryServer) requireNoCalls(t *testing.T, duration time.Duration) {
	t.Helper()
	select {
	case <-f.calls:
		t.Fatal("unexpected feed query call")
	case <-time.After(duration):
	}
}

func newTestClient(t *testing.T, query oracletypes.QueryServer, cfg Config) *Client {
	t.Helper()

	endpoint := newTestQueryEndpoint(t, "bufnet", query)

	client, err := NewClient(
		cfg,
		withDialOptions(grpc.WithContextDialer(dialTestQueryEndpoints(endpoint))),
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
	Run(context.Context) error
	Feeds() ([]string, error)
}

func startClient(t *testing.T, client pollingClient) context.CancelFunc {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.Run(ctx)
	}()

	return func() {
		cancel()
		select {
		case err := <-errCh:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("chain state client did not stop")
		}
	}
}

func stopClient(cancel context.CancelFunc, _ pollingClient) {
	cancel()
}

func requireEventuallyTargets(
	t *testing.T,
	client interface {
		Feeds() ([]string, error)
	},
	want []string,
) {
	t.Helper()

	require.Eventually(t, func() bool {
		got, err := client.Feeds()
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
