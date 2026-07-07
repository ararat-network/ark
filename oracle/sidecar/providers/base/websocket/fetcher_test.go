package websocket_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderwebsocket "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	sidecarinternal "noah/oracle/sidecar/internal"
	basewebsocket "noah/oracle/sidecar/providers/base/websocket"
	wstestutil "noah/oracle/sidecar/providers/base/websocket/testutil"
	"noah/oracle/sidecar/providers/types"
)

func TestNewFetcherValidatesInputs(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)

	tests := []struct {
		name        string
		cfg         basewebsocket.Config
		handler     basewebsocket.DataHandler
		opts        []basewebsocket.Option
		errContains string
	}{
		{
			name:        "nil data handler",
			cfg:         websocketConfig("wss://example.invalid"),
			errContains: "data handler is nil",
		},
		{
			name:        "nil dial function",
			cfg:         websocketConfig("wss://example.invalid"),
			handler:     handler,
			opts:        []basewebsocket.Option{basewebsocket.WithDialFunc(nil)},
			errContains: "dial function is nil",
		},
		{
			name:        "no endpoints",
			cfg:         websocketConfigWithEndpoints(nil),
			handler:     handler,
			errContains: "websocket endpoints cannot be empty",
		},
		{
			name:        "empty endpoint URL",
			cfg:         websocketConfigWithEndpoints([]types.Endpoint{{URL: ""}}),
			handler:     handler,
			errContains: "endpoint 0: endpoint url cannot be empty",
		},
		{
			name:    "valid",
			cfg:     websocketConfig("wss://example.invalid"),
			handler: handler,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher, err := basewebsocket.NewFetcher(tt.cfg, tt.handler, tt.opts...)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.cfg.Name, fetcher.Name())
			require.Equal(t, "websocket", string(fetcher.Type()))
		})
	}
}

func TestRunRejectsNilResponseChannelAndEmptyTickers(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)

	fetcher, err := basewebsocket.NewFetcher(websocketConfig("wss://example.invalid"), handler)
	require.NoError(t, err)

	require.ErrorContains(t, fetcher.Run(context.Background(), []types.Ticker{"ATOMUSD"}, nil), "response channel is nil")
	require.NoError(t, fetcher.Run(context.Background(), nil, make(chan types.Response)))
}

func TestResponseBufferSizeReturnsConfiguredMaxBufferSize(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)

	cfg := websocketConfig("wss://example.invalid")
	cfg.MaxBufferSize = 7
	fetcher, err := basewebsocket.NewFetcher(cfg, handler)
	require.NoError(t, err)

	require.Equal(t, 7, fetcher.ResponseBufferSize([]types.Ticker{"ATOMUSD"}))
}

func TestRunPublishesDialErrorResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	handler.EXPECT().Copy().Return(handler)

	cfg := websocketConfig("wss://example.invalid")
	cfg.ReconnectionTimeout = time.Hour
	fetcher, err := basewebsocket.NewFetcher(
		cfg,
		handler,
		basewebsocket.WithDialFunc(func(context.Context, string, *coderwebsocket.DialOptions) (*coderwebsocket.Conn, *http.Response, error) {
			return nil, nil, errors.New("dial failed")
		}),
	)
	require.NoError(t, err)

	response, err := runUntilResponse(fetcher, []types.Ticker{"ATOMUSD"})
	require.NoError(t, err)
	require.Equal(t, types.ErrorWebsocketStartFail, response.Unresolved["ATOMUSD"].Code())
}

func TestRunReturnsErrorWhenConnectionPanics(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	handler.EXPECT().
		Copy().
		DoAndReturn(func() basewebsocket.DataHandler {
			panic("connection exploded")
		})

	fetcher, err := basewebsocket.NewFetcher(websocketConfig("wss://example.invalid"), handler)
	require.NoError(t, err)

	err = fetcher.Run(context.Background(), []types.Ticker{"ATOMUSD"}, make(chan types.Response, 1))
	require.ErrorContains(t, err, "websocket connection panicked")
	require.ErrorContains(t, err, "connection exploded")
	require.True(t, sidecarinternal.IsPanic(err))
}

func TestRunReconnectsAfterDialError(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	handler.EXPECT().Copy().Return(handler).AnyTimes()

	var attempts atomic.Int32
	cfg := websocketConfig("wss://example.invalid")
	cfg.ReconnectionTimeout = time.Millisecond
	fetcher, err := basewebsocket.NewFetcher(
		cfg,
		handler,
		basewebsocket.WithDialFunc(func(context.Context, string, *coderwebsocket.DialOptions) (*coderwebsocket.Conn, *http.Response, error) {
			attempts.Add(1)
			return nil, nil, errors.New("dial failed")
		}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responseCh := make(chan types.Response, 2)
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, []types.Ticker{"ATOMUSD"}, responseCh)
	}()

	for range 2 {
		select {
		case response := <-responseCh:
			require.Equal(t, types.ErrorWebsocketStartFail, response.Unresolved["ATOMUSD"].Code())
		case err := <-errCh:
			t.Fatalf("fetcher stopped before retrying dial error: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for dial error response")
		}
	}

	cancel()
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("fetcher did not stop")
	}
	require.GreaterOrEqual(t, attempts.Load(), int32(2))
}

func TestRunPublishesSubscribeWriteErrorResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}
	closed := make(chan struct{})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return([][]byte{[]byte("subscribe")}, nil)

	server := websocketServer(t, func(conn *coderwebsocket.Conn) {
		if err := conn.CloseNow(); err != nil {
			t.Errorf("CloseNow() error = %v", err)
		}
		close(closed)
	})
	cfg := websocketConfig(server.URL)
	fetcher, err := basewebsocket.NewFetcher(
		cfg,
		handler,
		basewebsocket.WithDialFunc(func(ctx context.Context, url string, opts *coderwebsocket.DialOptions) (*coderwebsocket.Conn, *http.Response, error) {
			conn, resp, err := coderwebsocket.Dial(ctx, url, opts)
			if err != nil {
				return nil, resp, err
			}
			<-closed
			if err := conn.CloseNow(); err != nil {
				return nil, resp, err
			}
			return conn, resp, nil
		}),
	)
	require.NoError(t, err)

	response, err := runUntilResponse(fetcher, tickers)
	require.NoError(t, err)
	require.Equal(t, types.ErrorWebsocketStartFail, response.Unresolved["ATOMUSD"].Code())
}

func TestRunPublishesHandledMessageAndWritesUpdate(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}
	expected := types.NewResponse(map[types.Ticker]types.Result{
		"ATOMUSD": types.NewResult(nil, time.Unix(10, 0).UTC()),
	}, nil)

	readUpdate := make(chan []byte, 1)
	server := websocketServer(t, func(conn *coderwebsocket.Conn) {
		_, subscription, err := conn.Read(context.Background())
		require.NoError(t, err)
		require.Equal(t, []byte("subscribe"), subscription)

		require.NoError(t, conn.Write(context.Background(), coderwebsocket.MessageText, []byte("price")))

		_, update, err := conn.Read(context.Background())
		require.NoError(t, err)
		readUpdate <- update
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return([][]byte{[]byte("subscribe")}, nil)
	handler.EXPECT().HandleMessage([]byte("price")).Return(expected, [][]byte{[]byte("update")}, nil)

	fetcher, err := basewebsocket.NewFetcher(websocketConfig(server.URL), handler)
	require.NoError(t, err)

	response, err := runUntilResponse(fetcher, tickers)
	require.NoError(t, err)
	require.Equal(t, expected, response)
	require.Equal(t, []byte("update"), <-readUpdate)
}

func TestRunReturnsErrorWhenReceivePanics(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}

	server := websocketServer(t, func(conn *coderwebsocket.Conn) {
		require.NoError(t, conn.Write(context.Background(), coderwebsocket.MessageText, []byte("price")))
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().
		HandleMessage([]byte("price")).
		DoAndReturn(func([]byte) (types.Response, [][]byte, error) {
			panic("receive exploded")
		})

	cfg := websocketConfig(server.URL)
	cfg.PingInterval = time.Hour
	fetcher, err := basewebsocket.NewFetcher(cfg, handler)
	require.NoError(t, err)

	err = fetcher.Run(context.Background(), tickers, make(chan types.Response, 1))
	require.ErrorContains(t, err, "websocket receive panicked")
	require.ErrorContains(t, err, "receive exploded")
	require.True(t, sidecarinternal.IsPanic(err))
}

func TestRunSkipsParseErrorAndPublishesNextValidMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}
	expected := types.NewResponse(map[types.Ticker]types.Result{
		"ATOMUSD": types.NewResult(nil, time.Unix(10, 0).UTC()),
	}, nil)

	server := websocketServer(t, func(conn *coderwebsocket.Conn) {
		require.NoError(t, conn.Write(context.Background(), coderwebsocket.MessageText, []byte("bad")))
		require.NoError(t, conn.Write(context.Background(), coderwebsocket.MessageText, []byte("good")))
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().HandleMessage([]byte("bad")).Return(types.Response{}, nil, errors.New("parse failed"))
	handler.EXPECT().HandleMessage([]byte("good")).Return(expected, nil, nil)

	fetcher, err := basewebsocket.NewFetcher(websocketConfig(server.URL), handler)
	require.NoError(t, err)

	response, err := runUntilResponse(fetcher, tickers)
	require.NoError(t, err)
	require.Equal(t, expected, response)
}

func TestRunPublishesUnresolvedResponseAfterMaxReadErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD"}

	server := websocketServer(t, func(conn *coderwebsocket.Conn) {
		if err := conn.CloseNow(); err != nil {
			t.Errorf("CloseNow() error = %v", err)
		}
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)

	cfg := websocketConfig(server.URL)
	cfg.MaxReadErrorCount = 1
	fetcher, err := basewebsocket.NewFetcher(cfg, handler)
	require.NoError(t, err)

	response, err := runUntilResponse(fetcher, tickers)
	require.NoError(t, err)
	require.Empty(t, response.Resolved)
	require.Len(t, response.Unresolved, len(tickers))
	for _, ticker := range tickers {
		result, ok := response.Unresolved[ticker]
		require.True(t, ok)
		require.Equal(t, types.ErrorWebSocketGeneral, result.Code())
	}
}

func TestRunReturnsErrorWhenHeartbeatPanics(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}

	server := websocketServer(t, func(conn *coderwebsocket.Conn) {
		_, _, _ = conn.Read(context.Background())
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().
		HeartBeatMessages().
		DoAndReturn(func() ([][]byte, error) {
			panic("heartbeat exploded")
		})

	cfg := websocketConfig(server.URL)
	cfg.PingInterval = time.Millisecond
	cfg.ReadTimeout = time.Hour
	fetcher, err := basewebsocket.NewFetcher(cfg, handler)
	require.NoError(t, err)

	err = fetcher.Run(context.Background(), tickers, make(chan types.Response, 1))
	require.ErrorContains(t, err, "websocket heartbeat panicked")
	require.ErrorContains(t, err, "heartbeat exploded")
	require.True(t, sidecarinternal.IsPanic(err))
}

func TestRunSendsHeartbeatMessages(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}

	readHeartbeat := make(chan []byte, 1)
	server := websocketServer(t, func(conn *coderwebsocket.Conn) {
		_, heartbeat, err := conn.Read(context.Background())
		require.NoError(t, err)
		readHeartbeat <- heartbeat
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().HeartBeatMessages().Return([][]byte{[]byte("ping")}, nil).AnyTimes()

	cfg := websocketConfig(server.URL)
	cfg.PingInterval = time.Millisecond
	fetcher, err := basewebsocket.NewFetcher(cfg, handler)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, make(chan types.Response, 1))
	}()

	require.Equal(t, []byte("ping"), <-readHeartbeat)
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func runUntilResponse(fetcher *basewebsocket.Fetcher, tickers []types.Ticker) (types.Response, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	responseCh := make(chan types.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, responseCh)
	}()

	var response types.Response
	select {
	case response = <-responseCh:
	case err := <-errCh:
		return types.Response{}, err
	case <-time.After(time.Second):
		return types.Response{}, errors.New("timed out waiting for websocket response")
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(time.Second):
		return types.Response{}, errors.New("timed out waiting for websocket fetcher shutdown")
	}

	return response, nil
}

func websocketConfig(url string) basewebsocket.Config {
	return basewebsocket.Config{
		Name:                     "test",
		MaxBufferSize:            basewebsocket.DefaultMaxBufferSize,
		ReconnectionTimeout:      time.Hour,
		PostConnectionTimeout:    0,
		HandshakeTimeout:         basewebsocket.DefaultHandshakeTimeout,
		EnableCompression:        basewebsocket.DefaultEnableCompression,
		ReadTimeout:              basewebsocket.DefaultReadTimeout,
		WriteTimeout:             basewebsocket.DefaultWriteTimeout,
		PingInterval:             basewebsocket.DefaultPingInterval,
		WriteInterval:            basewebsocket.DefaultWriteInterval,
		MaxReadErrorCount:        1,
		MaxTickersPerConnection:  basewebsocket.DefaultMaxTickersPerConnection,
		MaxSubscriptionsPerBatch: basewebsocket.DefaultMaxSubscriptionsPerBatch,
		Endpoints: []types.Endpoint{
			{URL: "ws" + strings.TrimPrefix(url, "http")},
		},
	}
}

func websocketConfigWithEndpoints(endpoints []types.Endpoint) basewebsocket.Config {
	config := websocketConfig("wss://example.invalid")
	config.Endpoints = endpoints
	return config
}

func websocketServer(t *testing.T, handle func(*coderwebsocket.Conn)) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderwebsocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			if err := conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("CloseNow() error = %v", err)
			}
		}()
		handle(conn)
	}))
	t.Cleanup(server.Close)

	return server
}
