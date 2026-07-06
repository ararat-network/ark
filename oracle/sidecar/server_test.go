package sidecar_test

import (
	"context"
	"errors"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/version"

	server "noah/oracle/sidecar"
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

func TestVersion(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())
	response, err := server.Version(context.Background(), &transporttypes.OracleVersionRequest{})

	require.NoError(t, err)
	require.Equal(t, version.Version, response.Version)
}

func TestPricesRejectsNilRequest(t *testing.T) {
	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())

	response, err := server.Prices(context.Background(), nil)

	require.Nil(t, response)
	require.ErrorIs(t, err, transporttypes.ErrNilRequest)
}

func TestPricesRejectsStoppedOracle(t *testing.T) {
	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.Nil(t, response)
	require.ErrorIs(t, err, transporttypes.ErrOracleNotRunning)
}

func TestPrices(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	runtime := newTestRuntime(t, oracletypes.Prices{
		"ARK/USD": mustBigFloat(t, "123.456"),
		"ARK/KRW": mustBigFloat(t, "42.25"),
	})
	startTestRuntime(t, runtime)
	requireRuntimeTick(t, runtime)
	server := server.NewOracleServer(runtime, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)
	require.Equal(t, math.LegacyMustNewDecFromStr("123.456"), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("42.25"), decodePrice(t, response.Prices["ukrw"]))
}

func TestPricesReturnsZeroPricesForMissingVoteTargets(t *testing.T) {
	runtime := newTestRuntime(t, oracletypes.Prices{})
	startTestRuntime(t, runtime)
	requireRuntimeTick(t, runtime)
	server := server.NewOracleServer(runtime, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.Equal(t, math.LegacyZeroDec(), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyZeroDec(), decodePrice(t, response.Prices["ukrw"]))
	require.False(t, response.Timestamp.IsZero())
	require.Equal(t, version.Version, response.Version)
}

func TestCloseIsIdempotent(t *testing.T) {
	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())

	require.NoError(t, server.Close())
	require.NoError(t, server.Close())

	select {
	case <-server.Done():
	default:
		t.Fatal("server Done channel was not closed")
	}
}

func TestStartServerRejectsInvalidPort(t *testing.T) {
	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())

	err := server.StartServer(context.Background(), "127.0.0.1", "invalid")

	require.Error(t, err)
}

func TestStartServerWithListenerRejectsInvalidAddress(t *testing.T) {
	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())

	err := server.StartServerWithListener(context.Background(), invalidAddrListener{})

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

	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.StartServerWithListener(context.Background(), listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}

	require.NoError(t, server.Close())

	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("server did not close")
	}

	select {
	case err := <-serverErrCh:
		require.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(time.Second):
		t.Fatal("server did not stop after Close")
	}
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

	server := server.NewOracleServer(newTestRuntime(t, nil), log.NewNopLogger())
	ctx, cancel := context.WithCancel(context.Background())
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.StartServerWithListener(ctx, listener)
	}()

	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}

	cancel()

	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("server did not close after context cancellation")
	}

	select {
	case err := <-serverErrCh:
		require.True(t, err == nil || errors.Is(err, http.ErrServerClosed), "unexpected server error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
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

type serverTestChainStateClient struct {
	denoms []string
}

func (c *serverTestChainStateClient) Start(context.Context) error {
	return nil
}

func (c *serverTestChainStateClient) Stop() {}

func (c *serverTestChainStateClient) Update(chainstate.Config) {}

func (c *serverTestChainStateClient) VoteTargets() ([]string, error) {
	return append([]string(nil), c.denoms...), nil
}

type serverTestResolver struct {
	prices oracletypes.Prices
}

func newServerTestResolver(prices oracletypes.Prices) *serverTestResolver {
	copied := make(oracletypes.Prices, len(prices))
	for pair, price := range prices {
		if price == nil {
			copied[pair] = nil
			continue
		}
		copied[pair] = new(big.Float).Copy(price)
	}

	return &serverTestResolver{prices: copied}
}

func (r *serverTestResolver) SetProviderPrices(string, oracletypes.Prices) {}

func (r *serverTestResolver) ResolvePrices([]string) {}

func (r *serverTestResolver) GetPrices() oracletypes.Prices {
	copied := make(oracletypes.Prices, len(r.prices))
	for pair, price := range r.prices {
		if price == nil {
			copied[pair] = nil
			continue
		}
		copied[pair] = new(big.Float).Copy(price)
	}

	return copied
}

func (r *serverTestResolver) Update(resolver.Config) {}

func (r *serverTestResolver) Reset() {}

func newTestRuntime(t *testing.T, prices oracletypes.Prices) *runtimepkg.Runtime {
	t.Helper()

	markets := providertypes.Markets{
		{Pair: "ARK/USD", Symbol: "ARKUSD"},
		{Pair: "ARK/KRW", Symbol: "ARKKRW"},
	}
	provider, err := base.NewProvider("test", base.API, markets, serverTestFetcher{})
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
	runtime, err := runtimepkg.NewRuntime(
		cfg,
		runtimepkg.WithProviders(provider),
		runtimepkg.WithResolver(newServerTestResolver(prices)),
		runtimepkg.WithChainStateClient(&serverTestChainStateClient{denoms: cfg.FallbackDenoms}),
	)
	require.NoError(t, err)

	return runtime
}

func startTestRuntime(t *testing.T, runtime *runtimepkg.Runtime) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.Start(ctx)
	}()

	require.Eventually(t, runtime.IsRunning, time.Second, time.Millisecond)
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("runtime did not stop")
		}
	})
}

func requireRuntimeTick(t *testing.T, runtime *runtimepkg.Runtime) {
	t.Helper()

	require.Eventually(t, func() bool {
		return !runtime.GetLastSyncTime().IsZero()
	}, time.Second, time.Millisecond)
}
