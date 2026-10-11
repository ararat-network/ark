package websocket_test

import (
	"context"
	"errors"
	"fmt"
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

	sidecarinternal "github.com/ararat-network/ark/pricefeed/sidecar/internal"
	basewebsocket "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	wstestutil "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket/testutil"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
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

func TestRunAppliesSelectedEndpointAuthenticationAtDialTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	handler.EXPECT().Copy().Return(handler)

	cfg := websocketConfig("wss://example.invalid")
	cfg.Endpoints = []types.Endpoint{
		{
			URL: "wss://first.example.invalid",
			Authentication: types.Authentication{
				APIKeyHeader: "X-First-Key",
				APIKey:       "first-secret",
			},
		},
		{
			URL: "wss://second.example.invalid",
			Authentication: types.Authentication{
				APIKeyHeader: "X-Second-Key",
				APIKey:       "second-secret",
			},
		},
	}

	type dialAttempt struct {
		url     string
		headers http.Header
	}
	attempts := make(chan dialAttempt, 1)
	staticHeaders := http.Header{
		"X-Static":     []string{"static"},
		"X-Second-Key": []string{"stale-secret"},
	}

	fetcher, err := basewebsocket.NewFetcher(
		cfg,
		handler,
		basewebsocket.WithHeaders(staticHeaders),
		basewebsocket.WithEndpointSelector(func(endpoints []types.Endpoint) (types.Endpoint, error) {
			return endpoints[1], nil
		}),
		basewebsocket.WithDialFunc(func(_ context.Context, url string, opts *coderwebsocket.DialOptions) (*coderwebsocket.Conn, *http.Response, error) {
			attempts <- dialAttempt{url: url, headers: opts.HTTPHeader.Clone()}
			return nil, nil, errors.New("dial failed")
		}),
	)
	require.NoError(t, err)

	_, err = runUntilResponse(fetcher, []types.Ticker{"ATOMUSD"})
	require.NoError(t, err)

	attempt := <-attempts
	require.Equal(t, cfg.Endpoints[1].URL, attempt.url)
	require.Empty(t, attempt.headers.Get("X-First-Key"))
	require.Equal(t, "second-secret", attempt.headers.Get("X-Second-Key"))
	require.Equal(t, "static", attempt.headers.Get("X-Static"))
	require.Equal(t, "stale-secret", staticHeaders.Get("X-Second-Key"))
}

func TestRunPublishesSubscribeWriteErrorResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}
	closed := make(chan struct{})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return([][]byte{[]byte("subscribe")}, nil)

	server, _ := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		defer close(closed)
		return conn.CloseNow()
	})
	cfg := websocketConfig(strings.Replace(server.URL, "https://", "wss://", 1))
	fetcher, err := basewebsocket.NewFetcher(
		cfg,
		handler,
		basewebsocket.WithHTTPClient(server.Client()),
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

	subscriptions := make(chan []byte, 1)
	readUpdate := make(chan []byte, 1)
	server, serverErrs := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		_, subscription, err := conn.Read(context.Background())
		if err != nil {
			return err
		}
		subscriptions <- subscription

		if err := conn.Write(context.Background(), coderwebsocket.MessageText, []byte("price")); err != nil {
			return err
		}

		_, update, err := conn.Read(context.Background())
		if err != nil {
			return err
		}
		readUpdate <- update
		return nil
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return([][]byte{[]byte("subscribe")}, nil)
	handler.EXPECT().HandleMessage([]byte("price")).Return(expected, [][]byte{[]byte("update")}, nil)

	fetcher, err := basewebsocket.NewFetcher(websocketConfig(strings.Replace(server.URL, "https://", "wss://", 1)), handler, basewebsocket.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	// The fetcher publishes the response before it writes the update, so keep
	// it running until the server has read the update.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responseCh := make(chan types.Response, 1)
	errCh := make(chan error, 1)
	go func() { errCh <- fetcher.Run(ctx, tickers, responseCh) }()

	select {
	case response := <-responseCh:
		require.Equal(t, expected, response)
	case err := <-errCh:
		t.Fatalf("fetcher stopped before responding: %v", err)
	case err := <-serverErrs:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for websocket response")
	}
	require.Equal(t, []byte("subscribe"), <-subscriptions)
	select {
	case update := <-readUpdate:
		require.Equal(t, []byte("update"), update)
	case err := <-serverErrs:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the update write")
	}
}

func TestRunReturnsErrorWhenReceivePanics(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}

	server, _ := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		return conn.Write(context.Background(), coderwebsocket.MessageText, []byte("price"))
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().
		HandleMessage([]byte("price")).
		DoAndReturn(func([]byte) (types.Response, [][]byte, error) {
			panic("receive exploded")
		})

	cfg := websocketConfig(strings.Replace(server.URL, "https://", "wss://", 1))
	cfg.PingInterval = time.Hour
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(server.Client()))
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

	server, _ := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		if err := conn.Write(context.Background(), coderwebsocket.MessageText, []byte("bad")); err != nil {
			return err
		}
		return conn.Write(context.Background(), coderwebsocket.MessageText, []byte("good"))
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().HandleMessage([]byte("bad")).Return(types.Response{}, nil, errors.New("parse failed"))
	handler.EXPECT().HandleMessage([]byte("good")).Return(expected, nil, nil)

	fetcher, err := basewebsocket.NewFetcher(websocketConfig(strings.Replace(server.URL, "https://", "wss://", 1)), handler, basewebsocket.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	response, err := runUntilResponse(fetcher, tickers)
	require.NoError(t, err)
	require.Equal(t, expected, response)
}

func TestRunPublishesUnresolvedResponseAfterReadError(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD"}

	server, _ := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		return conn.CloseNow()
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)

	cfg := websocketConfig(strings.Replace(server.URL, "https://", "wss://", 1))
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(server.Client()))
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

	server, _ := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		_, _, _ = conn.Read(context.Background())
		return nil
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().
		HeartBeatMessages().
		DoAndReturn(func() ([][]byte, error) {
			panic("heartbeat exploded")
		})

	cfg := websocketConfig(strings.Replace(server.URL, "https://", "wss://", 1))
	cfg.PingInterval = time.Millisecond
	cfg.ReadTimeout = time.Hour
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(server.Client()))
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
	server, serverErrs := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		_, heartbeat, err := conn.Read(context.Background())
		if err != nil {
			return err
		}
		readHeartbeat <- heartbeat
		return nil
	})

	handler.EXPECT().Copy().Return(handler)
	handler.EXPECT().CreateMessages(tickers).Return(nil, nil)
	handler.EXPECT().HeartBeatMessages().Return([][]byte{[]byte("ping")}, nil).AnyTimes()

	cfg := websocketConfig(strings.Replace(server.URL, "https://", "wss://", 1))
	cfg.PingInterval = time.Millisecond
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, make(chan types.Response, 1))
	}()

	select {
	case heartbeat := <-readHeartbeat:
		require.Equal(t, []byte("ping"), heartbeat)
	case err := <-serverErrs:
		t.Fatalf("server: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the heartbeat")
	}
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

// A server that subscribes and then stays silent trips ReadTimeout: the
// tickers are reported unresolved and the session is redialled.
func TestRunRedialsAfterReadTimeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}
	handler.EXPECT().Copy().Return(handler).MinTimes(1)
	handler.EXPECT().CreateMessages(tickers).Return([][]byte{[]byte("subscribe")}, nil).MinTimes(1)

	accepted := make(chan struct{}, 8)
	server, serverErrs := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		accepted <- struct{}{}
		if _, _, err := conn.Read(context.Background()); err != nil {
			return fmt.Errorf("reading subscription: %w", err)
		}
		// Silent until the fetcher gives up on the connection.
		_, _, _ = conn.Read(context.Background())
		return nil
	})
	cfg := websocketConfig(server.URL)
	cfg.ReadTimeout = 20 * time.Millisecond
	cfg.ReconnectionTimeout = time.Millisecond
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	responses, stop := runFetcher(t, fetcher, tickers)
	response := receiveWebsocketResponse(t, responses, serverErrs)
	require.Empty(t, response.Resolved)
	unresolved := response.Unresolved["ATOMUSD"]
	require.Equal(t, types.ErrorWebSocketGeneral, unresolved.Code())
	require.ErrorContains(t, unresolved, basewebsocket.ErrRead.Error())
	require.ErrorContains(t, unresolved, context.DeadlineExceeded.Error())
	receiveSignals(t, accepted, 2)
	require.ErrorIs(t, stop(), context.Canceled)
}

// A heartbeat that cannot be written reports the tickers unresolved with the
// write failure and redials.
func TestRunRedialsAfterHeartbeatWriteFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}
	handler.EXPECT().Copy().Return(handler).MinTimes(1)
	handler.EXPECT().CreateMessages(tickers).Return([][]byte{[]byte("subscribe")}, nil).MinTimes(1)
	handler.EXPECT().HeartBeatMessages().Return([][]byte{[]byte("ping")}, nil).AnyTimes()

	subscribed := make(chan struct{}, 8)
	server, serverErrs := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		if _, _, err := conn.Read(context.Background()); err != nil {
			return fmt.Errorf("reading subscription: %w", err)
		}
		subscribed <- struct{}{}
		for {
			if _, _, err := conn.Read(context.Background()); err != nil {
				return nil
			}
		}
	})
	conns := make(chan *failableConn, 8)
	cfg := websocketConfig(server.URL)
	cfg.PingInterval = 5 * time.Millisecond
	cfg.ReadTimeout = time.Hour
	cfg.ReconnectionTimeout = time.Millisecond
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(failableClient(server, conns)))
	require.NoError(t, err)

	responses, stop := runFetcher(t, fetcher, tickers)
	first := receiveConn(t, conns)
	receiveSignals(t, subscribed, 1)
	first.failWrites()

	requireWriteFailure(t, responses, serverErrs, "ATOMUSD")
	receiveConn(t, conns)
	require.ErrorIs(t, stop(), context.Canceled)
}

// A follow-up message the handler asks for that cannot be written reports the
// tickers unresolved with the write failure and redials.
func TestRunRedialsAfterUpdateWriteFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD"}
	handler.EXPECT().Copy().Return(handler).MinTimes(1)
	handler.EXPECT().CreateMessages(tickers).Return([][]byte{[]byte("subscribe")}, nil).MinTimes(1)
	handler.EXPECT().HandleMessage([]byte("price")).Return(types.Response{}, [][]byte{[]byte("update")}, nil).MinTimes(1)

	subscribed := make(chan struct{}, 8)
	send := make(chan struct{})
	server, serverErrs := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		if _, _, err := conn.Read(context.Background()); err != nil {
			return fmt.Errorf("reading subscription: %w", err)
		}
		subscribed <- struct{}{}
		select {
		case <-send:
		case <-time.After(5 * time.Second):
			return errors.New("never told to send")
		}
		if err := conn.Write(context.Background(), coderwebsocket.MessageText, []byte("price")); err != nil {
			return fmt.Errorf("writing price: %w", err)
		}
		_, _, _ = conn.Read(context.Background())
		return nil
	})
	conns := make(chan *failableConn, 8)
	cfg := websocketConfig(server.URL)
	cfg.ReadTimeout = time.Hour
	cfg.ReconnectionTimeout = time.Millisecond
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(failableClient(server, conns)))
	require.NoError(t, err)

	responses, stop := runFetcher(t, fetcher, tickers)
	first := receiveConn(t, conns)
	receiveSignals(t, subscribed, 1)
	first.failWrites()
	send <- struct{}{}

	requireWriteFailure(t, responses, serverErrs, "ATOMUSD")
	receiveConn(t, conns)
	close(send)
	require.ErrorIs(t, stop(), context.Canceled)
}

// MaxTickersPerConnection shards tickers across connections, each with its own
// handler copy and subscription.
func TestRunShardsTickersAcrossConnections(t *testing.T) {
	ctrl := gomock.NewController(t)
	handler := wstestutil.NewMockDataHandler(ctrl)
	first := wstestutil.NewMockDataHandler(ctrl)
	second := wstestutil.NewMockDataHandler(ctrl)
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}
	gomock.InOrder(
		handler.EXPECT().Copy().Return(first),
		handler.EXPECT().Copy().Return(second),
	)
	// The two connections start in either order, so each copy may serve either shard.
	for _, copied := range []*wstestutil.MockDataHandler{first, second} {
		copied.EXPECT().CreateMessages(gomock.Any()).DoAndReturn(func(shard []types.Ticker) ([][]byte, error) {
			return [][]byte{[]byte(strings.Join(tickerStrings(shard), ","))}, nil
		})
	}

	subscriptions := make(chan string, 8)
	server, serverErrs := websocketServer(t, func(conn *coderwebsocket.Conn) error {
		_, subscription, err := conn.Read(context.Background())
		if err != nil {
			return fmt.Errorf("reading subscription: %w", err)
		}
		subscriptions <- string(subscription)
		_, _, _ = conn.Read(context.Background())
		return nil
	})
	cfg := websocketConfig(server.URL)
	cfg.MaxTickersPerConnection = 2
	cfg.ReadTimeout = time.Hour
	fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, stop := runFetcher(t, fetcher, tickers)
	got := make([]string, 0, 2)
	for range 2 {
		select {
		case subscription := <-subscriptions:
			got = append(got, subscription)
		case err := <-serverErrs:
			t.Fatalf("server: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for subscriptions; got %v", got)
		}
	}
	require.ElementsMatch(t, []string{"ATOMUSD,BTCUSD", "ETHUSD"}, got)
	require.ErrorIs(t, stop(), context.Canceled)
}

// An inbound frame is read up to 32 KiB; a larger one fails the read, reports
// the tickers unresolved, and is never handed to the handler.
func TestRunBoundsInboundFrameSize(t *testing.T) {
	const limit = 32 << 10
	tests := []struct {
		name    string
		size    int
		handled bool
	}{
		{name: "at the limit", size: limit, handled: true},
		{name: "over the limit", size: limit + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			handler := wstestutil.NewMockDataHandler(ctrl)
			tickers := []types.Ticker{"ATOMUSD"}
			frame := []byte(strings.Repeat("x", tt.size))
			handled := types.NewResponse(map[types.Ticker]types.Result{
				"ATOMUSD": types.NewResult(nil, time.Unix(10, 0).UTC()),
			}, nil)
			handler.EXPECT().Copy().Return(handler).AnyTimes()
			handler.EXPECT().CreateMessages(tickers).Return(nil, nil).AnyTimes()
			if tt.handled {
				handler.EXPECT().HandleMessage(frame).Return(handled, nil, nil)
			}

			server, serverErrs := websocketServer(t, func(conn *coderwebsocket.Conn) error {
				if err := conn.Write(context.Background(), coderwebsocket.MessageText, frame); err != nil {
					return fmt.Errorf("writing frame: %w", err)
				}
				_, _, _ = conn.Read(context.Background())
				return nil
			})
			cfg := websocketConfig(server.URL)
			cfg.ReadTimeout = time.Hour
			fetcher, err := basewebsocket.NewFetcher(cfg, handler, basewebsocket.WithHTTPClient(server.Client()))
			require.NoError(t, err)

			responses, stop := runFetcher(t, fetcher, tickers)
			response := receiveWebsocketResponse(t, responses, serverErrs)
			if tt.handled {
				require.Equal(t, handled, response)
			} else {
				require.Empty(t, response.Resolved)
				unresolved := response.Unresolved["ATOMUSD"]
				require.Equal(t, types.ErrorWebSocketGeneral, unresolved.Code())
				require.ErrorContains(t, unresolved, basewebsocket.ErrRead.Error())
				require.ErrorContains(t, unresolved, fmt.Sprintf("read limited at %d bytes", limit+1))
			}
			require.ErrorIs(t, stop(), context.Canceled)
		})
	}
}

func tickerStrings(tickers []types.Ticker) []string {
	out := make([]string, len(tickers))
	for i, ticker := range tickers {
		out[i] = string(ticker)
	}
	return out
}

// runFetcher runs fetcher until stop, which cancels it and returns Run's error.
func runFetcher(t *testing.T, fetcher *basewebsocket.Fetcher, tickers []types.Ticker) (<-chan types.Response, func() error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	responses := make(chan types.Response, 8)
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, responses)
	}()
	t.Cleanup(cancel)

	return responses, func() error {
		cancel()
		select {
		case err := <-errCh:
			return err
		case <-time.After(5 * time.Second):
			return errors.New("timed out waiting for websocket fetcher shutdown")
		}
	}
}

func receiveWebsocketResponse(t *testing.T, responses <-chan types.Response, serverErrs <-chan error) types.Response {
	t.Helper()

	select {
	case response := <-responses:
		return response
	case err := <-serverErrs:
		t.Fatalf("server: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for websocket response")
	}
	return types.Response{}
}

// requireWriteFailure waits for a response reporting ticker unresolved by a
// write failure. Closing the connection after a failed write can also fail its
// read, so read failures on the way are skipped.
func requireWriteFailure(t *testing.T, responses <-chan types.Response, serverErrs <-chan error, ticker types.Ticker) {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case response := <-responses:
			unresolved, ok := response.Unresolved[ticker]
			require.True(t, ok, "response without %s: %v", ticker, response)
			require.Equal(t, types.ErrorWebSocketGeneral, unresolved.Code())
			if strings.Contains(unresolved.Error(), basewebsocket.ErrWrite.Error()) {
				require.ErrorContains(t, unresolved, errWriteRefused.Error())
				return
			}
			require.ErrorContains(t, unresolved, basewebsocket.ErrRead.Error())
		case err := <-serverErrs:
			t.Fatalf("server: %v", err)
		case <-deadline:
			t.Fatal("timed out waiting for the write failure")
		}
	}
}

func receiveSignals(t *testing.T, ch <-chan struct{}, n int) {
	t.Helper()

	for i := range n {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for signal %d of %d", i+1, n)
		}
	}
}

func receiveConn(t *testing.T, conns <-chan *failableConn) *failableConn {
	t.Helper()

	select {
	case conn := <-conns:
		return conn
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a dial")
		return nil
	}
}

var errWriteRefused = errors.New("write refused by test")

// failableConn is a client connection whose writes can be made to fail while
// its reads continue.
type failableConn struct {
	net.Conn

	fail atomic.Bool
}

func (c *failableConn) failWrites() {
	c.fail.Store(true)
}

func (c *failableConn) Write(p []byte) (int, error) {
	if c.fail.Load() {
		return 0, errWriteRefused
	}
	return c.Conn.Write(p)
}

// failableClient dials server through failableConns, handing each to conns in
// dial order.
func failableClient(server *httptest.Server, conns chan<- *failableConn) *http.Client {
	transport := server.Client().Transport.(*http.Transport).Clone()
	var dialer net.Dialer
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		failable := &failableConn{Conn: conn}
		conns <- failable
		return failable, nil
	}
	return &http.Client{Transport: transport}
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
		MaxTickersPerConnection:  basewebsocket.DefaultMaxTickersPerConnection,
		MaxSubscriptionsPerBatch: basewebsocket.DefaultMaxSubscriptionsPerBatch,
		Endpoints: []types.Endpoint{
			{URL: strings.Replace(url, "https://", "wss://", 1)},
		},
	}
}

func websocketConfigWithEndpoints(endpoints []types.Endpoint) basewebsocket.Config {
	config := websocketConfig("wss://example.invalid")
	config.Endpoints = endpoints
	return config
}

// websocketServer serves handle on each accepted connection. Handlers run off
// the test goroutine, so their failures arrive on the returned channel.
func websocketServer(t *testing.T, handle func(*coderwebsocket.Conn) error) (*httptest.Server, <-chan error) {
	t.Helper()

	errs := make(chan error, 16)
	report := func(err error) {
		select {
		case errs <- err:
		default:
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderwebsocket.Accept(w, r, nil)
		if err != nil {
			report(fmt.Errorf("accept: %w", err))
			return
		}
		if err := handle(conn); err != nil {
			report(err)
		}
		if err := conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
			report(fmt.Errorf("close: %w", err))
		}
	}))
	t.Cleanup(server.Close)

	return server, errs
}
