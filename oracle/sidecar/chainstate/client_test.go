package chainstate_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

func TestUpdateConfigDropsStaleVoteTargetsFromPreviousAddress(t *testing.T) {
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
	require.ErrorContains(t, err, "no vote targets fetched yet")
	require.Nil(t, got)

	secondQuery.release()
	requireEventuallyTargets(t, client, []string{"ukrw"})
}

func TestStartRecoversPollPanicAndKeepsPolling(t *testing.T) {
	query := newFakeQueryServer(queryResult{targets: []string{"uusd"}})
	endpoint := newTestQueryEndpoint(t, "bufnet", query)
	var panicked atomic.Bool

	client, err := chainstate.NewClient(
		chainstate.Config{
			Address:  endpoint.address,
			Timeout:  time.Second,
			Interval: time.Millisecond,
		},
		chainstate.WithDialOptions(
			grpc.WithContextDialer(dialTestQueryEndpoints(endpoint)),
			grpc.WithUnaryInterceptor(func(
				ctx context.Context,
				method string,
				req any,
				reply any,
				cc *grpc.ClientConn,
				invoker grpc.UnaryInvoker,
				opts ...grpc.CallOption,
			) error {
				if panicked.CompareAndSwap(false, true) {
					panic("vote target query panic")
				}
				return invoker(ctx, method, req, reply, cc, opts...)
			}),
		),
	)
	require.NoError(t, err)

	cancel := startClient(t, client)
	defer stopClient(cancel, client)

	requireEventuallyTargets(t, client, []string{"uusd"})
	require.True(t, panicked.Load())
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
