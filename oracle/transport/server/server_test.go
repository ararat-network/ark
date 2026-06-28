package server_test

import (
	"context"
	"errors"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/stretchr/testify/require"

	"noah/oracle/transport/server"
	transporttypes "noah/oracle/transport/types"
	oracletypes "noah/oracle/types"
	"noah/pkg/encoding"
)

type testOracle struct {
	running      bool
	prices       oracletypes.Prices
	lastSyncTime time.Time
	getPrices    func() oracletypes.Prices
}

func (o testOracle) IsRunning() bool            { return o.running }
func (o testOracle) GetLastSyncTime() time.Time { return o.lastSyncTime }
func (o testOracle) GetPrices() oracletypes.Prices {
	if o.getPrices != nil {
		return o.getPrices()
	}
	return o.prices
}
func (o testOracle) Start(context.Context) error { return nil }
func (o testOracle) Stop()                       {}

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

	server := server.NewOracleServer(testOracle{}, log.NewNopLogger())
	response, err := server.Version(context.Background(), &transporttypes.OracleVersionRequest{})

	require.NoError(t, err)
	require.Equal(t, version.Version, response.Version)
}

func TestPricesRejectsNilRequest(t *testing.T) {
	server := server.NewOracleServer(testOracle{}, log.NewNopLogger())

	response, err := server.Prices(context.Background(), nil)

	require.Nil(t, response)
	require.ErrorIs(t, err, transporttypes.ErrNilRequest)
}

func TestPricesRejectsStoppedOracle(t *testing.T) {
	server := server.NewOracleServer(testOracle{running: false}, log.NewNopLogger())

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

	lastSyncTime := time.Date(2026, time.June, 19, 12, 30, 0, 0, time.UTC)
	server := server.NewOracleServer(testOracle{
		running: true,
		prices: oracletypes.Prices{
			"BTC/USD": mustBigFloat(t, "123.456"),
			"ETH/USD": mustBigFloat(t, "42.25"),
		},
		lastSyncTime: lastSyncTime,
	}, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.Equal(t, lastSyncTime, response.Timestamp)
	require.Equal(t, version.Version, response.Version)
	require.Equal(t, math.LegacyMustNewDecFromStr("123.456"), decodePrice(t, response.Prices["BTC/USD"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("42.25"), decodePrice(t, response.Prices["ETH/USD"]))
}

func TestPricesReturnsEmptyResponse(t *testing.T) {
	lastSyncTime := time.Date(2026, time.June, 20, 12, 30, 0, 0, time.UTC)
	server := server.NewOracleServer(testOracle{
		running:      true,
		prices:       oracletypes.Prices{},
		lastSyncTime: lastSyncTime,
	}, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.Empty(t, response.Prices)
	require.Equal(t, lastSyncTime, response.Timestamp)
	require.Equal(t, version.Version, response.Version)
}

func TestPricesReturnsConversionError(t *testing.T) {
	server := server.NewOracleServer(testOracle{
		running: true,
		prices: oracletypes.Prices{
			"BTC/USD": (*big.Float)(nil),
		},
	}, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.Nil(t, response)
	require.ErrorContains(t, err, "convert oracle prices")
	require.ErrorContains(t, err, "nil price for BTC/USD")
}

func TestPricesReturnsContextError(t *testing.T) {
	getPricesStarted := make(chan struct{})
	releaseGetPrices := make(chan struct{})
	server := server.NewOracleServer(testOracle{
		running: true,
		getPrices: func() oracletypes.Prices {
			close(getPricesStarted)
			<-releaseGetPrices
			return oracletypes.Prices{}
		},
	}, log.NewNopLogger())

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := server.Prices(ctx, &transporttypes.OraclePricesRequest{})
		resultCh <- err
	}()

	<-getPricesStarted
	cancel()

	require.ErrorIs(t, <-resultCh, context.Canceled)
	close(releaseGetPrices)
}

func TestPricesReturnsDeadlineExceeded(t *testing.T) {
	getPricesStarted := make(chan struct{})
	releaseGetPrices := make(chan struct{})
	server := server.NewOracleServer(testOracle{
		running: true,
		getPrices: func() oracletypes.Prices {
			close(getPricesStarted)
			<-releaseGetPrices
			return oracletypes.Prices{}
		},
	}, log.NewNopLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	resultCh := make(chan error, 1)
	go func() {
		_, err := server.Prices(ctx, &transporttypes.OraclePricesRequest{})
		resultCh <- err
	}()

	<-getPricesStarted

	require.ErrorIs(t, <-resultCh, context.DeadlineExceeded)
	close(releaseGetPrices)
}

func TestCloseIsIdempotent(t *testing.T) {
	server := server.NewOracleServer(testOracle{}, log.NewNopLogger())

	require.NoError(t, server.Close())
	require.NoError(t, server.Close())

	select {
	case <-server.Done():
	default:
		t.Fatal("server Done channel was not closed")
	}
}

func TestStartServerRejectsInvalidPort(t *testing.T) {
	server := server.NewOracleServer(testOracle{}, log.NewNopLogger())

	err := server.StartServer(context.Background(), "127.0.0.1", "invalid")

	require.Error(t, err)
}

func TestStartServerWithListenerRejectsInvalidAddress(t *testing.T) {
	server := server.NewOracleServer(testOracle{}, log.NewNopLogger())

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

	server := server.NewOracleServer(testOracle{}, log.NewNopLogger())
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

	server := server.NewOracleServer(testOracle{}, log.NewNopLogger())
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
