package sidecar

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/chainstate"
	"noah/oracle/sidecar/providers"
	frankfurterapi "noah/oracle/sidecar/providers/api/frankfurter"
	"noah/oracle/sidecar/providers/base"
	providertypes "noah/oracle/sidecar/providers/types"
	runtimepkg "noah/oracle/sidecar/runtime"
)

type closeRecordingListener struct {
	closed bool
	addr   net.Addr
}

func (*closeRecordingListener) Accept() (net.Conn, error) {
	return nil, errors.New("listener should not accept connections")
}

func (l *closeRecordingListener) Close() error {
	l.closed = true
	return nil
}

func (l *closeRecordingListener) Addr() net.Addr {
	if l.addr != nil {
		return l.addr
	}
	return fixedInternalAddr("127.0.0.1:0")
}

type fixedInternalAddr string

func (a fixedInternalAddr) Network() string {
	return "tcp"
}

func (a fixedInternalAddr) String() string {
	return string(a)
}

func TestStartPreServeFailureListenerOwnership(t *testing.T) {
	testCases := []struct {
		name       string
		start      func(*Oracle, net.Listener) error
		wantClosed bool
	}{
		{
			name: "owned listener is closed",
			start: func(oracle *Oracle, listener net.Listener) error {
				return oracle.startOwnedListener(context.Background(), listener)
			},
			wantClosed: true,
		},
		{
			name: "caller-owned listener stays open",
			start: func(oracle *Oracle, listener net.Listener) error {
				return oracle.StartWithListener(context.Background(), listener)
			},
			wantClosed: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testInternalRuntimeConfig()
			oracle, err := NewOracle(cfg, log.NewNopLogger())
			require.NoError(t, err)
			listener := &closeRecordingListener{addr: fixedInternalAddr("invalid-address")}

			err = tc.start(oracle, listener)

			require.ErrorContains(t, err, "[grpc server]: invalid listener address")
			require.Equal(t, tc.wantClosed, listener.closed)
		})
	}
}

func TestUpdateRejectsClosingOracle(t *testing.T) {
	cfg := testInternalRuntimeConfig()
	oracle, err := NewOracle(cfg, log.NewNopLogger())
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
	})

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	httpSrv := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			startedOnce.Do(func() {
				close(started)
			})
			<-release
			w.WriteHeader(http.StatusOK)
		}),
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
		_ = httpSrv.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, oracle.startLifecycle(cancel, httpSrv, grpc.NewServer(), nil))

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- httpSrv.Serve(listener)
	}()
	clientErrCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
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

	closeErrCh := make(chan error, 1)
	go func() {
		closeErrCh <- oracle.Close()
	}()
	require.Eventually(t, func() bool {
		return ctx.Err() != nil
	}, time.Second, time.Millisecond)

	err = oracle.Update(cfg)

	require.ErrorIs(t, err, errOracleClosed)
	releaseOnce.Do(func() {
		close(release)
	})

	select {
	case err := <-closeErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("oracle close did not finish")
	}
	select {
	case err := <-serveErrCh:
		require.True(t, err == nil || errors.Is(err, http.ErrServerClosed), "unexpected serve error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("server did not stop after shutdown")
	}
	select {
	case err := <-clientErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("client request did not finish")
	}
}

func TestStartLifecycleRejectsClosingOracle(t *testing.T) {
	cfg := testInternalRuntimeConfig()
	oracle, err := NewOracle(cfg, log.NewNopLogger())
	require.NoError(t, err)

	oracle.lifecycleMu.Lock()
	oracle.closing = true
	oracle.lifecycleMu.Unlock()

	err = oracle.startLifecycle(func() {}, nil, grpc.NewServer(), nil)

	require.ErrorIs(t, err, errOracleClosed)
}

func TestCloseReturnsShutdownError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
	})

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	httpSrv := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			startedOnce.Do(func() {
				close(started)
			})
			<-release
			w.WriteHeader(http.StatusOK)
		}),
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
		_ = httpSrv.Close()
	})

	oracle := &Oracle{
		logger: log.NewNopLogger(),
		doneCh: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, oracle.startLifecycle(cancel, httpSrv, grpc.NewServer(), nil))

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- httpSrv.Serve(listener)
	}()

	clientErrCh := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
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

	err = oracle.Close()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, oracle.Close(), context.DeadlineExceeded)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	releaseOnce.Do(func() {
		close(release)
	})

	select {
	case err := <-serveErrCh:
		require.True(t, err == nil || errors.Is(err, http.ErrServerClosed), "unexpected serve error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("server did not stop after shutdown")
	}
	select {
	case err := <-clientErrCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("client request did not finish")
	}
}

func TestRecoverUnaryPanicMapsHandlerErrorsToStatusCodes(t *testing.T) {
	oracle := &Oracle{logger: log.NewNopLogger()}

	testCases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{
			name: "nil request",
			err:  ErrNilRequest,
			code: codes.InvalidArgument,
		},
		{
			name: "oracle not running",
			err:  ErrOracleNotRunning,
			code: codes.Unavailable,
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			code: codes.Canceled,
		},
		{
			name: "deadline exceeded",
			err:  context.DeadlineExceeded,
			code: codes.DeadlineExceeded,
		},
		{
			name: "existing status error",
			err:  status.Error(codes.ResourceExhausted, "rate limited"),
			code: codes.ResourceExhausted,
		},
		{
			name: "unexpected handler error",
			err:  errors.New("conversion failed"),
			code: codes.Internal,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := oracle.recoverUnaryPanic(
				context.Background(),
				nil,
				&grpc.UnaryServerInfo{FullMethod: "/noah.transport.v1.Oracle/Prices"},
				func(context.Context, any) (any, error) {
					return nil, tc.err
				},
			)

			require.Nil(t, response)
			require.Equal(t, tc.code, status.Code(err))
		})
	}
}

func testInternalRuntimeConfig() runtimepkg.Config {
	providerCfg := providers.Config{
		Name:          frankfurterapi.Name,
		TransportType: base.API,
		Markets: providertypes.Markets{
			{Pair: "ARK/USD", Symbol: "ARKUSD"},
		},
		API: frankfurterapi.DefaultAPIConfig,
	}

	return runtimepkg.Config{
		UpdateInterval: time.Second,
		MaxPriceAge:    time.Minute,
		Providers: map[string]providers.Config{
			providerCfg.Name: providerCfg,
		},
		Client: chainstate.Config{
			Address:  "passthrough:///oracle",
			Timeout:  time.Second,
			Interval: time.Second,
		},
		FallbackDenoms: []string{"uusd"},
	}
}
