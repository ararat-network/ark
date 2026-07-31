package sidecar

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"cosmossdk.io/log/v2"

	transporttypes "ark/oracle/types"
)

type recordingOracleService struct {
	transporttypes.UnimplementedOracleServer

	version     string
	pricesErr   error
	pricesPanic any
}

func (s *recordingOracleService) Prices(
	context.Context,
	*transporttypes.OraclePricesRequest,
) (*transporttypes.OraclePricesResponse, error) {
	if s.pricesPanic != nil {
		panic(s.pricesPanic)
	}
	return nil, s.pricesErr
}

func (s *recordingOracleService) Version(
	context.Context,
	*transporttypes.OracleVersionRequest,
) (*transporttypes.OracleVersionResponse, error) {
	return &transporttypes.OracleVersionResponse{Version: s.version}, nil
}

func startBufferedServer(
	t *testing.T,
	srv *server,
) (*http.Client, context.CancelFunc, <-chan error) {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	dial := func(ctx context.Context) (net.Conn, error) {
		return listener.DialContext(ctx)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dial(ctx)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.serve(
			ctx,
			listener,
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return dial(ctx)
			}),
		)
	}()
	t.Cleanup(func() {
		cancel()
		client.CloseIdleConnections()
	})

	return client, cancel, errCh
}

func startTestServer(t *testing.T, service transporttypes.OracleServer) *http.Client {
	t.Helper()

	srv, err := newServer(service, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)
	client, cancel, errCh := startBufferedServer(t, srv)
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Error("server did not stop")
		}
	})

	return client
}

type blockingListener struct {
	accepted  chan struct{}
	closed    chan struct{}
	acceptOne sync.Once
	closeOne  sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{
		accepted: make(chan struct{}),
		closed:   make(chan struct{}),
	}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	l.acceptOne.Do(func() {
		close(l.accepted)
	})
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.closeOne.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *blockingListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

func TestServerGatewayUsesInjectedOracleService(t *testing.T) {
	service := &recordingOracleService{version: "v1.2.3"}
	client := startTestServer(t, service)

	response, err := client.Get("http://oracle/ark/transport/v1/version")
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.JSONEq(t, `{"version":"v1.2.3"}`, string(body))
}

func TestServerGatewayAppliesGRPCInterceptor(t *testing.T) {
	testCases := []struct {
		name       string
		service    *recordingOracleService
		statusCode int
	}{
		{
			name:       "domain error",
			service:    &recordingOracleService{pricesErr: ErrOracleNotRunning},
			statusCode: http.StatusServiceUnavailable,
		},
		{
			name:       "panic",
			service:    &recordingOracleService{pricesPanic: "prices exploded"},
			statusCode: http.StatusInternalServerError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := startTestServer(t, tc.service)
			response, err := client.Get("http://oracle/ark/transport/v1/prices")
			require.NoError(t, err)
			defer response.Body.Close()

			require.Equal(t, tc.statusCode, response.StatusCode)
		})
	}
}

func TestNewServerAcceptsEphemeralPort(t *testing.T) {
	srv, err := newServer(&recordingOracleService{}, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)

	require.Equal(t, "127.0.0.1:0", srv.address)
}

func TestServerServeReturnsListenerError(t *testing.T) {
	srv, err := newServer(&recordingOracleService{}, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)
	listener := bufconn.Listen(1024 * 1024)
	require.NoError(t, listener.Close())

	err = srv.serve(context.Background(), listener)

	require.ErrorContains(t, err, "serve oracle requests")
}

func TestServerServeStopsOnContextCancellation(t *testing.T) {
	started := make(chan struct{})
	var startedOnce sync.Once
	httpSrv := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			startedOnce.Do(func() {
				close(started)
			})
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	t.Cleanup(func() {
		_ = httpSrv.Close()
	})
	srv, err := newServer(&recordingOracleService{}, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)
	srv.httpSrv = httpSrv

	client, cancel, errCh := startBufferedServer(t, srv)
	clientErrCh := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://oracle")
		if resp != nil {
			_ = resp.Body.Close()
		}
		clientErrCh <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("test request did not start")
	}

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case <-clientErrCh:
	case <-time.After(time.Second):
		t.Fatal("client request did not finish")
	}
}

func TestServerServeForceClosesActiveRequestOnCancel(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	var startedOnce sync.Once
	var stoppedOnce sync.Once

	httpSrv := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			startedOnce.Do(func() {
				close(started)
			})
			select {
			case <-r.Context().Done():
			case <-release:
			}
			stoppedOnce.Do(func() {
				close(stopped)
			})
		}),
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
		_ = httpSrv.Close()
	})

	srv, err := newServer(&recordingOracleService{}, log.NewNopLogger(), "127.0.0.1:0")
	require.NoError(t, err)
	srv.httpSrv = httpSrv
	client, cancel, runErrCh := startBufferedServer(t, srv)
	clientErrCh := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://oracle")
		if resp != nil {
			_ = resp.Body.Close()
		}
		clientErrCh <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("test request did not start")
	}

	cancel()

	select {
	case err := <-runErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("server run did not stop after cancellation")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("active request was not closed after cancellation")
	}
	select {
	case <-clientErrCh:
	case <-time.After(time.Second):
		t.Fatal("client request did not finish after forced shutdown")
	}
}

// TestServerServesInitialCommittedSnapshot intentionally exercises the actual
// h2c/gRPC transport. Direct RPC tests run only the runtime and do not need a
// listener.
func TestServerServesInitialCommittedSnapshot(t *testing.T) {
	cfg := newTestRuntimeConfig()
	cfg.UpdateInterval = time.Hour
	oracle := newTestOracleFromRuntime(
		t,
		cfg,
		newServerTestFetcher(nil),
		newStaticChainStateClient(cfg.FallbackFeeds),
		ProcessConfig{ServerAddress: "127.0.0.1:0"},
	)
	startTestRuntime(t, oracle)

	listener := bufconn.Listen(1024 * 1024)
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.server.serve(ctx, listener, grpc.WithContextDialer(dialer))
	}()

	conn, err := grpc.NewClient(
		"passthrough:///oracle",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})
	client := transporttypes.NewOracleClient(conn)

	var response *transporttypes.OraclePricesResponse
	require.Eventually(t, func() bool {
		response, err = client.Prices(context.Background(), &transporttypes.OraclePricesRequest{})
		return err == nil
	}, time.Second, time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, response)
	require.True(t, response.Timestamp.IsZero())
	require.Equal(t, Version(), response.Version)

	versionResponse, err := client.Version(context.Background(), &transporttypes.OracleVersionRequest{})
	require.NoError(t, err)
	require.Equal(t, Version(), versionResponse.Version)

	cancel()
	requireOracleStopped(t, errCh)
}
