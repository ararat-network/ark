package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers/types"
)

type noopDataHandler struct{}

func (noopDataHandler) HandleMessage([]byte) (types.Response, [][]byte, error) {
	return types.Response{}, nil, nil
}

func (noopDataHandler) CreateMessages([]types.Ticker) ([][]byte, error) {
	return nil, nil
}

func (noopDataHandler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
}

func (h noopDataHandler) Copy() DataHandler {
	return h
}

type heartbeatDataHandler struct{}

func (heartbeatDataHandler) HandleMessage([]byte) (types.Response, [][]byte, error) {
	return types.Response{}, nil, nil
}

func (heartbeatDataHandler) CreateMessages([]types.Ticker) ([][]byte, error) {
	return nil, nil
}

func (heartbeatDataHandler) HeartBeatMessages() ([][]byte, error) {
	return [][]byte{[]byte("ping")}, nil
}

func (h heartbeatDataHandler) Copy() DataHandler {
	return h
}

func TestHeartBeatReturnsWriteError(t *testing.T) {
	t.Parallel()

	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		if err := conn.CloseNow(); err != nil {
			t.Errorf("CloseNow() error = %v", err)
		}
		close(closed)
	}))
	defer server.Close()

	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() {
		if err := conn.CloseNow(); err != nil {
			t.Errorf("CloseNow() error = %v", err)
		}
	}()
	<-closed

	fetcher := &Fetcher{
		logger: log.NewNopLogger(),
		config: Config{
			Name:         "test",
			PingInterval: time.Millisecond,
			WriteTimeout: time.Second,
		},
	}

	err = fetcher.heartBeat(
		context.Background(),
		conn,
		heartbeatDataHandler{},
		[]types.Ticker{"ATOMUSD"},
		make(chan types.Response, 1),
	)
	if !errors.Is(err, errReconnect) {
		t.Fatalf("heartBeat() error = %v, want errReconnect", err)
	}
	if !errors.Is(err, ErrWrite) {
		t.Fatalf("heartBeat() error = %v, want ErrWrite", err)
	}
}

func TestRunOncePublishesUnresolvedResponseAfterMaxReadErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		if err := conn.CloseNow(); err != nil {
			t.Errorf("CloseNow() error = %v", err)
		}
	}))
	defer server.Close()

	tickers := []types.Ticker{"ATOMUSD", "BTCUSD"}
	fetcher, err := NewFetcher(
		Config{
			Name:                     "test",
			MaxBufferSize:            DefaultMaxBufferSize,
			ReconnectionTimeout:      DefaultReconnectionTimeout,
			PostConnectionTimeout:    DefaultPostConnectionTimeout,
			HandshakeTimeout:         DefaultHandshakeTimeout,
			EnableCompression:        DefaultEnableCompression,
			ReadTimeout:              DefaultReadTimeout,
			WriteTimeout:             DefaultWriteTimeout,
			PingInterval:             DefaultPingInterval,
			WriteInterval:            DefaultWriteInterval,
			MaxReadErrorCount:        1,
			MaxTickersPerConnection:  DefaultMaxTickersPerConnection,
			MaxSubscriptionsPerBatch: DefaultMaxSubscriptionsPerBatch,
			Endpoints: []types.Endpoint{
				{URL: "ws" + strings.TrimPrefix(server.URL, "http")},
			},
		},
		noopDataHandler{},
	)
	if err != nil {
		t.Fatalf("NewFetcher() error = %v", err)
	}

	responseCh := make(chan types.Response, 1)
	err = fetcher.runOnce(context.Background(), tickers, noopDataHandler{}, responseCh)
	if !errors.Is(err, errReconnect) {
		t.Fatalf("runOnce() error = %v, want errReconnect", err)
	}
	if !errors.Is(err, ErrRead) {
		t.Fatalf("runOnce() error = %v, want ErrRead", err)
	}

	select {
	case response := <-responseCh:
		if got := len(response.Resolved); got != 0 {
			t.Fatalf("resolved responses = %d, want 0", got)
		}
		if got := len(response.Unresolved); got != len(tickers) {
			t.Fatalf("unresolved responses = %d, want %d", got, len(tickers))
		}

		for _, ticker := range tickers {
			result, ok := response.Unresolved[ticker]
			if !ok {
				t.Fatalf("unresolved response missing ticker %s", ticker)
			}
			if got := result.Code(); got != types.ErrorWebSocketGeneral {
				t.Fatalf("unresolved response code = %d, want %d", got, types.ErrorWebSocketGeneral)
			}
		}
	default:
		t.Fatal("runOnce() did not publish unresolved response")
	}
}
