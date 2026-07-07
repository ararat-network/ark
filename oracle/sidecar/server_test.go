package sidecar_test

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"net"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/version"

	sidecar "noah/oracle/sidecar"
	"noah/oracle/sidecar/chainstate"
	"noah/oracle/sidecar/providers"
	"noah/oracle/sidecar/providers/base"
	baseapi "noah/oracle/sidecar/providers/base/api"
	providertypes "noah/oracle/sidecar/providers/types"
	"noah/oracle/sidecar/resolver"
	runtimepkg "noah/oracle/sidecar/runtime"
	oracletypes "noah/oracle/sidecar/types"
	transporttypes "noah/oracle/types"
	"noah/pkg/encoding"
)

type readyListener struct {
	net.Listener
	ready chan struct{}
	once  sync.Once
}

func (l *readyListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		close(l.ready)
	})
	return l.Listener.Accept()
}

type invalidAddrListener struct{}

func (invalidAddrListener) Accept() (net.Conn, error) {
	return nil, errors.New("listener should not accept connections")
}

func (invalidAddrListener) Close() error {
	return nil
}

func (invalidAddrListener) Addr() net.Addr {
	return invalidAddr{}
}

type invalidAddr struct{}

func (invalidAddr) Network() string {
	return "tcp"
}

func (invalidAddr) String() string {
	return "invalid-address"
}

type panicAcceptListener struct{}

func (panicAcceptListener) Accept() (net.Conn, error) {
	panic("listener exploded")
}

func (panicAcceptListener) Close() error {
	return nil
}

func (panicAcceptListener) Addr() net.Addr {
	return fixedAddr("127.0.0.1:0")
}

type fixedAddr string

func (a fixedAddr) Network() string {
	return "tcp"
}

func (a fixedAddr) String() string {
	return string(a)
}

func TestVersion(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	oracle := newTestOracle(t, nil)
	response, err := oracle.Version(context.Background(), &transporttypes.OracleVersionRequest{})

	require.NoError(t, err)
	require.Equal(t, version.Version, response.Version)
}

func TestPricesRejectsNilRequest(t *testing.T) {
	oracle := newTestOracle(t, nil)

	response, err := oracle.Prices(context.Background(), nil)

	require.Nil(t, response)
	require.ErrorIs(t, err, sidecar.ErrNilRequest)
}

func TestPricesRejectsStoppedOracle(t *testing.T) {
	oracle := newTestOracle(t, nil)

	response, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, sidecar.ErrOracleNotRunning)
}

func TestPrices(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	oracle := newTestOracle(t, oracletypes.Prices{
		"ARK/USD": mustBigFloat(t, "123.456"),
		"ARK/KRW": mustBigFloat(t, "42.25"),
	})
	startTestOracle(t, oracle)

	response := requireOracleTick(t, oracle)

	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)
	require.Equal(t, math.LegacyMustNewDecFromStr("123.456"), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("42.25"), decodePrice(t, response.Prices["ukrw"]))
}

func TestPricesReturnsZeroPricesForMissingVoteTargets(t *testing.T) {
	oracle := newTestOracle(t, oracletypes.Prices{})
	startTestOracle(t, oracle)

	response := requireOracleTick(t, oracle)

	require.Equal(t, math.LegacyZeroDec(), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyZeroDec(), decodePrice(t, response.Prices["ukrw"]))
	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)
}

func TestPricesReturnsCommittedPriceTimestampSnapshot(t *testing.T) {
	resolver := newBlockingCommitResolver(
		oracletypes.Prices{
			"ARK/USD": mustBigFloat(t, "1.25"),
			"ARK/KRW": mustBigFloat(t, "1300"),
		},
		oracletypes.Prices{
			"ARK/USD": mustBigFloat(t, "2.50"),
			"ARK/KRW": mustBigFloat(t, "2600"),
		},
	)
	cfg, opts := newTestRuntimeConfig(t, nil, serverTestFetcher{})
	cfg.UpdateInterval = time.Millisecond
	opts = append(opts, runtimepkg.WithResolver(resolver))
	oracle, err := sidecar.NewOracle(cfg, log.NewNopLogger(), opts...)
	require.NoError(t, err)
	startTestOracle(t, oracle)
	defer resolver.release()

	initial := requireOracleTick(t, oracle)
	require.Equal(t, math.LegacyMustNewDecFromStr("1.25"), decodePrice(t, initial.Prices["uusd"]))

	resolver.advance()
	requireSignal(t, resolver.blocked, "runtime did not block after resolving new prices")

	response, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})
	require.NoError(t, err)

	require.Equal(t, math.LegacyMustNewDecFromStr("1.25"), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("1300"), decodePrice(t, response.Prices["ukrw"]))
	require.False(t, response.Timestamp.IsZero())
}

func TestPricesReturnsContextErrorBeforePriceSnapshot(t *testing.T) {
	unexpectedResolver := &unexpectedGetPricesResolver{called: make(chan struct{})}
	cfg, opts := newTestRuntimeConfig(t, nil, serverTestFetcher{})
	cfg.UpdateInterval = time.Hour
	opts = append(opts, runtimepkg.WithResolver(unexpectedResolver))
	oracle, err := sidecar.NewOracle(cfg, log.NewNopLogger(), opts...)
	require.NoError(t, err)
	startTestOracle(t, oracle)
	require.Eventually(t, func() bool {
		response, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})
		return err == nil && response != nil
	}, time.Second, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	response, err := oracle.Prices(ctx, &transporttypes.OraclePricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, context.Canceled)
	require.Never(t, unexpectedResolver.wasCalled, 25*time.Millisecond, time.Millisecond)
}

func TestUpdateAppliesRuntimeConfig(t *testing.T) {
	cfg, opts := newTestRuntimeConfig(t, oracletypes.Prices{
		"ARK/USD": mustBigFloat(t, "123.456"),
		"ARK/KRW": mustBigFloat(t, "42.25"),
	}, serverTestFetcher{})
	client := &serverTestChainStateClient{
		denoms: cfg.FallbackDenoms,
		denomsByAddress: map[string][]string{
			"passthrough:///uusd-vote-targets": {"uusd"},
		},
	}
	opts = append(opts, runtimepkg.WithChainStateClient(client))
	oracle, err := sidecar.NewOracle(cfg, log.NewNopLogger(), opts...)
	require.NoError(t, err)
	startTestOracle(t, oracle)

	response := requireOracleTick(t, oracle)
	require.Contains(t, response.Prices, "uusd")
	require.Contains(t, response.Prices, "ukrw")

	newCfg := cfg
	newCfg.Client.Address = "passthrough:///uusd-vote-targets"
	require.NoError(t, oracle.Update(newCfg))

	require.Eventually(t, func() bool {
		response, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})
		if err != nil {
			return false
		}
		if len(response.Prices) != 1 {
			return false
		}
		price, ok := response.Prices["uusd"]
		return ok && decodePrice(t, price).Equal(math.LegacyMustNewDecFromStr("123.456"))
	}, time.Second, time.Millisecond)
}

func TestUpdateRejectsClosedOracle(t *testing.T) {
	cfg, opts := newTestRuntimeConfig(t, nil, serverTestFetcher{})
	oracle, err := sidecar.NewOracle(cfg, log.NewNopLogger(), opts...)
	require.NoError(t, err)
	require.NoError(t, oracle.Close())

	err = oracle.Update(cfg)

	require.EqualError(t, err, "oracle is closed")
}

func TestCloseIsIdempotent(t *testing.T) {
	oracle := newTestOracle(t, nil)

	require.NoError(t, oracle.Close())
	require.NoError(t, oracle.Close())

	select {
	case <-oracle.Done():
	default:
		t.Fatal("oracle Done channel was not closed")
	}
}

func TestStartServerRejectsInvalidPort(t *testing.T) {
	oracle := newTestOracle(t, nil)

	err := oracle.Start(context.Background(), "127.0.0.1", "invalid")

	require.Error(t, err)
}

func TestStartWithListenerRejectsInvalidAddress(t *testing.T) {
	oracle := newTestOracle(t, nil)

	err := oracle.StartWithListener(context.Background(), invalidAddrListener{})

	require.ErrorContains(t, err, "[grpc server]: invalid listener address")
}

func TestCloseStopsStartedServer(t *testing.T) {
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &readyListener{
		Listener: baseListener,
		ready:    make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	oracle := newTestOracle(t, nil)
	oracleErrCh := make(chan error, 1)
	go func() {
		oracleErrCh <- oracle.StartWithListener(context.Background(), listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}

	require.NoError(t, oracle.Close())

	select {
	case <-oracle.Done():
	case <-time.After(time.Second):
		t.Fatal("oracle did not close")
	}

	select {
	case err := <-oracleErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop after Close")
	}
}

func TestCloseStopsGatewayRegistrationContext(t *testing.T) {
	before := countGatewayRegistrationGoroutines()
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &readyListener{
		Listener: baseListener,
		ready:    make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	oracle := newTestOracle(t, nil)
	oracleErrCh := make(chan error, 1)
	go func() {
		oracleErrCh <- oracle.StartWithListener(context.Background(), listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}

	require.NoError(t, oracle.Close())

	select {
	case err := <-oracleErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop after Close")
	}

	require.Eventually(t, func() bool {
		return countGatewayRegistrationGoroutines() <= before
	}, time.Second, time.Millisecond, gatewayRegistrationStacks())
}

func TestContextCancellationClosesStartedServer(t *testing.T) {
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &readyListener{
		Listener: baseListener,
		ready:    make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	oracle := newTestOracle(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	oracleErrCh := make(chan error, 1)
	go func() {
		oracleErrCh <- oracle.StartWithListener(ctx, listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}

	cancel()

	select {
	case <-oracle.Done():
	case <-time.After(time.Second):
		t.Fatal("oracle did not close after context cancellation")
	}

	select {
	case err := <-oracleErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop after context cancellation")
	}
}

func TestStartWithListenerReturnsRuntimePanic(t *testing.T) {
	cfg, opts := newTestRuntimeConfig(t, nil, serverTestFetcher{})
	opts = append(opts, runtimepkg.WithChainStateClient(panicStartChainStateClient{}))
	oracle, err := sidecar.NewOracle(cfg, log.NewNopLogger(), opts...)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
	})

	err = oracle.StartWithListener(context.Background(), listener)

	require.ErrorContains(t, err, "oracle runtime panicked: vote-target client exploded")
	select {
	case <-oracle.Done():
	default:
		t.Fatal("oracle Done channel was not closed")
	}
}

func TestStartWithListenerReturnsTransportPanic(t *testing.T) {
	oracle := newTestOracle(t, nil)

	err := oracle.StartWithListener(context.Background(), panicAcceptListener{})

	require.ErrorContains(t, err, "oracle transport panicked: listener exploded")
	select {
	case <-oracle.Done():
	default:
		t.Fatal("oracle Done channel was not closed")
	}
}

func TestPricesReadsCommittedSnapshotWithoutResolver(t *testing.T) {
	cfg, opts := newTestRuntimeConfig(t, nil, serverTestFetcher{})
	cfg.UpdateInterval = time.Hour
	opts = append(opts, runtimepkg.WithResolver(panicGetPricesResolver{}))
	oracle, err := sidecar.NewOracle(cfg, log.NewNopLogger(), opts...)
	require.NoError(t, err)
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &readyListener{
		Listener: baseListener,
		ready:    make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	oracleErrCh := make(chan error, 1)
	go func() {
		oracleErrCh <- oracle.StartWithListener(context.Background(), listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})
	client := transporttypes.NewOracleClient(conn)

	response, err := client.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.NotNil(t, response)
	require.True(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)

	versionResp, err := client.Version(context.Background(), &transporttypes.OracleVersionRequest{})
	require.NoError(t, err)
	require.Equal(t, version.Version, versionResp.Version)

	require.NoError(t, oracle.Close())
	select {
	case err := <-oracleErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop")
	}
}

func TestDoneWaitsForRuntimeShutdown(t *testing.T) {
	fetcher := newDelayedShutdownFetcher()
	oracle := newTestOracleWithFetcher(t, nil, fetcher)
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &readyListener{
		Listener: baseListener,
		ready:    make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	oracleErrCh := make(chan error, 1)
	go func() {
		oracleErrCh <- oracle.StartWithListener(context.Background(), listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}
	select {
	case <-fetcher.started:
	case <-time.After(time.Second):
		t.Fatal("runtime fetcher did not start")
	}

	require.NoError(t, oracle.Close())

	select {
	case <-fetcher.stopped:
	case <-time.After(time.Second):
		t.Fatal("runtime fetcher was not stopped")
	}
	select {
	case <-oracle.Done():
		t.Fatal("oracle Done channel closed before runtime shutdown completed")
	default:
	}

	close(fetcher.release)

	select {
	case err := <-oracleErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle did not stop after delayed runtime shutdown was released")
	}
	select {
	case <-oracle.Done():
	case <-time.After(time.Second):
		t.Fatal("oracle Done channel was not closed after shutdown completed")
	}
}

func mustBigFloat(t *testing.T, value string) *big.Float {
	t.Helper()

	price, _, err := big.ParseFloat(value, 10, 256, big.ToNearestEven)
	require.NoError(t, err)
	return price
}

func decodePrice(t *testing.T, rawPrice []byte) math.LegacyDec {
	t.Helper()

	price, err := encoding.DecodeLegacyDec(rawPrice)
	require.NoError(t, err)
	return price
}

type serverTestFetcher struct{}

func (serverTestFetcher) Run(
	ctx context.Context,
	_ []providertypes.Ticker,
	_ chan<- providertypes.Response,
) error {
	<-ctx.Done()
	return ctx.Err()
}

func (serverTestFetcher) Type() base.TransportType { return base.API }

func (serverTestFetcher) Name() string { return "test" }

func (serverTestFetcher) ResponseBufferSize([]providertypes.Ticker) int { return 1 }

type panicStartChainStateClient struct{}

func (panicStartChainStateClient) Start(context.Context) error {
	panic("vote-target client exploded")
}

func (panicStartChainStateClient) Stop() {}

func (panicStartChainStateClient) Update(chainstate.Config) {}

func (panicStartChainStateClient) VoteTargets() ([]string, error) {
	return []string{"uusd"}, nil
}

type panicGetPricesResolver struct{}

func (panicGetPricesResolver) SetProviderPrices(string, oracletypes.Prices) {}

func (panicGetPricesResolver) ResolvePrices([]string) {}

func (panicGetPricesResolver) GetPrices() oracletypes.Prices {
	panic("prices exploded")
}

func (panicGetPricesResolver) Update(resolver.Config) {}

func (panicGetPricesResolver) Reset() {}

type unexpectedGetPricesResolver struct {
	called     chan struct{}
	calledOnce sync.Once
}

func (r *unexpectedGetPricesResolver) SetProviderPrices(string, oracletypes.Prices) {}

func (r *unexpectedGetPricesResolver) ResolvePrices([]string) {}

func (r *unexpectedGetPricesResolver) GetPrices() oracletypes.Prices {
	r.calledOnce.Do(func() {
		close(r.called)
	})
	return oracletypes.Prices{}
}

func (r *unexpectedGetPricesResolver) Update(resolver.Config) {}

func (r *unexpectedGetPricesResolver) Reset() {}

func (r *unexpectedGetPricesResolver) wasCalled() bool {
	select {
	case <-r.called:
		return true
	default:
		return false
	}
}

type blockingCommitResolver struct {
	mut sync.Mutex

	current oracletypes.Prices
	next    oracletypes.Prices

	advanced         bool
	resolvedAdvanced bool
	blockingStarted  bool

	blocked   chan struct{}
	releaseCh chan struct{}
}

func newBlockingCommitResolver(current, next oracletypes.Prices) *blockingCommitResolver {
	return &blockingCommitResolver{
		current:   copyOraclePrices(current),
		next:      copyOraclePrices(next),
		blocked:   make(chan struct{}),
		releaseCh: make(chan struct{}),
	}
}

func (r *blockingCommitResolver) SetProviderPrices(string, oracletypes.Prices) {}

func (r *blockingCommitResolver) ResolvePrices([]string) {
	r.mut.Lock()
	defer r.mut.Unlock()

	if r.advanced && !r.resolvedAdvanced {
		r.current = copyOraclePrices(r.next)
		r.resolvedAdvanced = true
	}
}

func (r *blockingCommitResolver) GetPrices() oracletypes.Prices {
	r.mut.Lock()
	prices := copyOraclePrices(r.current)
	shouldBlock := r.resolvedAdvanced && !r.blockingStarted
	if shouldBlock {
		r.blockingStarted = true
		close(r.blocked)
	}
	r.mut.Unlock()

	if shouldBlock {
		<-r.releaseCh
	}

	return prices
}

func (r *blockingCommitResolver) Update(resolver.Config) {}

func (r *blockingCommitResolver) Reset() {}

func (r *blockingCommitResolver) advance() {
	r.mut.Lock()
	defer r.mut.Unlock()

	r.advanced = true
}

func (r *blockingCommitResolver) release() {
	close(r.releaseCh)
}

type delayedShutdownFetcher struct {
	started   chan struct{}
	stopped   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

func newDelayedShutdownFetcher() *delayedShutdownFetcher {
	return &delayedShutdownFetcher{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (f *delayedShutdownFetcher) Run(
	ctx context.Context,
	_ []providertypes.Ticker,
	_ chan<- providertypes.Response,
) error {
	f.startOnce.Do(func() {
		close(f.started)
	})
	<-ctx.Done()
	f.stopOnce.Do(func() {
		close(f.stopped)
	})
	<-f.release
	return ctx.Err()
}

func (*delayedShutdownFetcher) Type() base.TransportType { return base.API }

func (*delayedShutdownFetcher) Name() string { return "test" }

func (*delayedShutdownFetcher) ResponseBufferSize([]providertypes.Ticker) int { return 1 }

type serverTestChainStateClient struct {
	mut             sync.RWMutex
	denoms          []string
	denomsByAddress map[string][]string
}

func (c *serverTestChainStateClient) Start(context.Context) error {
	return nil
}

func (c *serverTestChainStateClient) Stop() {}

func (c *serverTestChainStateClient) Update(cfg chainstate.Config) {
	c.mut.Lock()
	defer c.mut.Unlock()

	if denoms, ok := c.denomsByAddress[cfg.Address]; ok {
		c.denoms = append([]string(nil), denoms...)
	}
}

func (c *serverTestChainStateClient) VoteTargets() ([]string, error) {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return append([]string(nil), c.denoms...), nil
}

type serverTestResolver struct {
	prices oracletypes.Prices
}

func newServerTestResolver(prices oracletypes.Prices) *serverTestResolver {
	return &serverTestResolver{prices: copyOraclePrices(prices)}
}

func (r *serverTestResolver) SetProviderPrices(string, oracletypes.Prices) {}

func (r *serverTestResolver) ResolvePrices([]string) {}

func (r *serverTestResolver) GetPrices() oracletypes.Prices {
	return copyOraclePrices(r.prices)
}

func copyOraclePrices(prices oracletypes.Prices) oracletypes.Prices {
	copied := make(oracletypes.Prices, len(prices))
	for pair, price := range prices {
		if price == nil {
			copied[pair] = nil
			continue
		}
		copied[pair] = new(big.Float).Copy(price)
	}

	return copied
}

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func (r *serverTestResolver) Update(resolver.Config) {}

func (r *serverTestResolver) Reset() {}

func newTestOracle(t *testing.T, prices oracletypes.Prices) *sidecar.Oracle {
	t.Helper()

	return newTestOracleWithFetcher(t, prices, serverTestFetcher{})
}

func newTestOracleWithFetcher(t *testing.T, prices oracletypes.Prices, fetcher base.Fetcher) *sidecar.Oracle {
	t.Helper()

	cfg, opts := newTestRuntimeConfig(t, prices, fetcher)
	oracle, err := sidecar.NewOracle(cfg, log.NewNopLogger(), opts...)
	require.NoError(t, err)

	return oracle
}

func newTestRuntimeConfig(
	t *testing.T,
	prices oracletypes.Prices,
	fetcher base.Fetcher,
) (runtimepkg.Config, []runtimepkg.Option) {
	t.Helper()

	markets := providertypes.Markets{
		{Pair: "ARK/USD", Symbol: "ARKUSD"},
		{Pair: "ARK/KRW", Symbol: "ARKKRW"},
	}
	provider, err := base.NewProvider("test", base.API, markets, fetcher)
	require.NoError(t, err)

	cfg := runtimepkg.Config{
		UpdateInterval: 10 * time.Millisecond,
		MaxPriceAge:    time.Minute,
		Providers: map[string]providers.Config{
			"test": {
				Name:          "test",
				TransportType: base.API,
				Markets:       markets,
				API: baseapi.Config{
					Name:      "test",
					Timeout:   time.Second,
					Interval:  time.Hour,
					Endpoints: []providertypes.Endpoint{{URL: "https://example.invalid/prices"}},
				},
			},
		},
		Client: chainstate.Config{
			Address:  "passthrough:///vote-targets",
			Timeout:  time.Second,
			Interval: time.Hour,
		},
		FallbackDenoms: []string{"uusd", "ukrw"},
	}
	opts := []runtimepkg.Option{
		runtimepkg.WithProviders(provider),
		runtimepkg.WithResolver(newServerTestResolver(prices)),
		runtimepkg.WithChainStateClient(&serverTestChainStateClient{denoms: cfg.FallbackDenoms}),
	}

	return cfg, opts
}

func startTestOracle(t *testing.T, oracle *sidecar.Oracle) {
	t.Helper()

	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listener := &readyListener{
		Listener: baseListener,
		ready:    make(chan struct{}),
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.StartWithListener(context.Background(), listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("oracle server did not start")
	}
	t.Cleanup(func() {
		require.NoError(t, oracle.Close())
		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("oracle did not stop")
		}
	})
}

func requireOracleTick(t *testing.T, oracle *sidecar.Oracle) *transporttypes.OraclePricesResponse {
	t.Helper()

	var response *transporttypes.OraclePricesResponse
	require.Eventually(t, func() bool {
		resp, err := oracle.Prices(context.Background(), &transporttypes.OraclePricesRequest{})
		if err != nil || resp.Timestamp.IsZero() {
			return false
		}
		response = resp
		return true
	}, time.Second, time.Millisecond)

	return response
}

func countGatewayRegistrationGoroutines() int {
	stacks := gatewayRegistrationStacks()
	count := 0
	for _, stack := range strings.Split(stacks, "\n\n") {
		if strings.Contains(stack, "noah/oracle/types.RegisterOracleHandlerFromEndpoint") {
			count++
		}
	}
	return count
}

func gatewayRegistrationStacks() string {
	var buf bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&buf, 2)
	return buf.String()
}
