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

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	server "noah/oracle/sidecar"
	servertestutil "noah/oracle/sidecar/testutil"
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

func newMockOracleProvider(t *testing.T) *servertestutil.MockoracleProvider {
	t.Helper()

	return servertestutil.NewMockoracleProvider(gomock.NewController(t))
}

func TestVersion(t *testing.T) {
	originalVersion := version.Version
	t.Cleanup(func() {
		version.Version = originalVersion
	})
	version.Version = "v1.2.3"

	server := server.NewOracleServer(newMockOracleProvider(t), log.NewNopLogger())
	response, err := server.Version(context.Background(), &transporttypes.OracleVersionRequest{})

	require.NoError(t, err)
	require.Equal(t, version.Version, response.Version)
}

func TestPricesRejectsNilRequest(t *testing.T) {
	server := server.NewOracleServer(newMockOracleProvider(t), log.NewNopLogger())

	response, err := server.Prices(context.Background(), nil)

	require.Nil(t, response)
	require.ErrorIs(t, err, transporttypes.ErrNilRequest)
}

func TestPricesRejectsStoppedOracle(t *testing.T) {
	oracleProvider := newMockOracleProvider(t)
	oracleProvider.EXPECT().IsRunning().Return(false)
	server := server.NewOracleServer(oracleProvider, log.NewNopLogger())

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
	oracleProvider := newMockOracleProvider(t)
	oracleProvider.EXPECT().IsRunning().Return(true)
	oracleProvider.EXPECT().GetPrices().Return(oracletypes.DenomPrices{
		"uusd": mustBigFloat(t, "123.456"),
		"ukrw": mustBigFloat(t, "42.25"),
	})
	oracleProvider.EXPECT().GetLastSyncTime().Return(lastSyncTime)
	server := server.NewOracleServer(oracleProvider, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.Equal(t, lastSyncTime, response.Timestamp)
	require.Equal(t, version.Version, response.Version)
	require.Equal(t, math.LegacyMustNewDecFromStr("123.456"), decodePrice(t, response.Prices["uusd"]))
	require.Equal(t, math.LegacyMustNewDecFromStr("42.25"), decodePrice(t, response.Prices["ukrw"]))
}

func TestPricesReturnsEmptyResponse(t *testing.T) {
	lastSyncTime := time.Date(2026, time.June, 20, 12, 30, 0, 0, time.UTC)
	oracleProvider := newMockOracleProvider(t)
	oracleProvider.EXPECT().IsRunning().Return(true)
	oracleProvider.EXPECT().GetPrices().Return(oracletypes.DenomPrices{})
	oracleProvider.EXPECT().GetLastSyncTime().Return(lastSyncTime)
	server := server.NewOracleServer(oracleProvider, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.NoError(t, err)
	require.Empty(t, response.Prices)
	require.Equal(t, lastSyncTime, response.Timestamp)
	require.Equal(t, version.Version, response.Version)
}

func TestPricesReturnsConversionError(t *testing.T) {
	oracleProvider := newMockOracleProvider(t)
	oracleProvider.EXPECT().IsRunning().Return(true)
	oracleProvider.EXPECT().GetPrices().Return(oracletypes.DenomPrices{
		"uusd": (*big.Float)(nil),
	})
	server := server.NewOracleServer(oracleProvider, log.NewNopLogger())

	response, err := server.Prices(context.Background(), &transporttypes.OraclePricesRequest{})

	require.Nil(t, response)
	require.ErrorContains(t, err, "convert oracle prices")
	require.ErrorContains(t, err, "nil price for uusd")
}

func TestPricesReturnsContextError(t *testing.T) {
	getPricesStarted := make(chan struct{})
	releaseGetPrices := make(chan struct{})
	oracleProvider := newMockOracleProvider(t)
	oracleProvider.EXPECT().IsRunning().Return(true)
	oracleProvider.EXPECT().
		GetPrices().
		DoAndReturn(func() oracletypes.DenomPrices {
			close(getPricesStarted)
			<-releaseGetPrices
			return oracletypes.DenomPrices{}
		})
	oracleProvider.EXPECT().GetLastSyncTime().Return(time.Time{}).AnyTimes()
	server := server.NewOracleServer(oracleProvider, log.NewNopLogger())

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
	oracleProvider := newMockOracleProvider(t)
	oracleProvider.EXPECT().IsRunning().Return(true)
	oracleProvider.EXPECT().
		GetPrices().
		DoAndReturn(func() oracletypes.DenomPrices {
			close(getPricesStarted)
			<-releaseGetPrices
			return oracletypes.DenomPrices{}
		})
	oracleProvider.EXPECT().GetLastSyncTime().Return(time.Time{}).AnyTimes()
	server := server.NewOracleServer(oracleProvider, log.NewNopLogger())

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
	server := server.NewOracleServer(newMockOracleProvider(t), log.NewNopLogger())

	require.NoError(t, server.Close())
	require.NoError(t, server.Close())

	select {
	case <-server.Done():
	default:
		t.Fatal("server Done channel was not closed")
	}
}

func TestStartServerRejectsInvalidPort(t *testing.T) {
	server := server.NewOracleServer(newMockOracleProvider(t), log.NewNopLogger())

	err := server.StartServer(context.Background(), "127.0.0.1", "invalid")

	require.Error(t, err)
}

func TestStartServerWithListenerRejectsInvalidAddress(t *testing.T) {
	server := server.NewOracleServer(newMockOracleProvider(t), log.NewNopLogger())

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

	server := server.NewOracleServer(newMockOracleProvider(t), log.NewNopLogger())
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

	server := server.NewOracleServer(newMockOracleProvider(t), log.NewNopLogger())
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
