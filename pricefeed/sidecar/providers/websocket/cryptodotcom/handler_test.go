package cryptodotcom_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/cryptodotcom"
)

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name        string
		logger      log.Logger
		config      func() websocket.Config
		errContains string
	}{
		{
			name:   "default config",
			logger: log.NewNopLogger(),
			config: func() websocket.Config { return DefaultWebSocketConfig },
		},
		{
			name:        "nil logger",
			config:      func() websocket.Config { return DefaultWebSocketConfig },
			errContains: "logger is nil",
		},
		{
			name:   "config for another provider",
			logger: log.NewNopLogger(),
			config: func() websocket.Config {
				cfg := DefaultWebSocketConfig
				cfg.Name = "someone_else_ws"
				return cfg
			},
			errContains: "expected websocket config name",
		},
		{
			name:   "invalid config",
			logger: log.NewNopLogger(),
			config: func() websocket.Config {
				cfg := DefaultWebSocketConfig
				cfg.Endpoints = nil
				return cfg
			},
			errContains: "invalid websocket config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := NewHandler(tt.logger, tt.config())
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				require.Nil(t, handler)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, handler)
		})
	}
}

func TestCreateMessages(t *testing.T) {
	handler := newTestHandler(t)

	messages, err := handler.CreateMessages([]types.Ticker{"USDT_USD", "BTC_USD"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	for i, channel := range []string{"ticker.USDT_USD", "ticker.BTC_USD"} {
		var request SubscribeRequestMessage
		require.NoError(t, json.Unmarshal(messages[i], &request))
		require.Equal(t, int64(i+1), request.ID)
		require.Equal(t, "subscribe", request.Method)
		require.Equal(t, []string{channel}, request.Params.Channels)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no channels to subscribe to")
}

func TestHeartBeatMessagesAreNil(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Nil(t, messages)
}

// TestServerHeartbeatIsAnswered pins the liveness contract: the server
// heartbeat must be answered with its own id or the connection is closed.
func TestServerHeartbeatIsAnswered(t *testing.T) {
	handler := newSubscribedHandler(t, "USDT_USD")

	resp, toSend, err := handler.HandleMessage([]byte(`{"id":1587523073344,"method":"public/heartbeat","code":0}`))
	require.NoError(t, err)
	require.True(t, resp.Empty())
	require.Len(t, toSend, 1)
	require.JSONEq(t, `{"id":1587523073344,"method":"public/respond-heartbeat"}`, string(toSend[0]))
}

func TestHandleMessage(t *testing.T) {
	tests := []struct {
		name         string
		message      string
		wantResolved map[types.Ticker]string
		wantErrors   map[types.Ticker]types.ErrorCode
		errContains  string
	}{
		{
			name: "ticker update resolves the latest trade price",
			message: `{"id":-1,"method":"subscribe","code":0,"result":{"instrument_name":"USDT_USD","subscription":"ticker.USDT_USD","channel":"ticker",
				"data":[{"h":"1.0002","l":"0.9998","a":"1.000100","c":"0.0001","i":"USDT_USD","t":1613580710768}]}}`,
			wantResolved: map[types.Ticker]string{"USDT_USD": "1.0001"},
		},
		{
			name:    "subscription acknowledgement is ignored",
			message: `{"id":1,"method":"subscribe","code":0}`,
		},
		{
			name:        "error code is reported",
			message:     `{"id":1,"method":"subscribe","code":40003}`,
			errContains: "crypto.com error code 40003 for subscribe request 1",
		},
		{
			name:       "untraded instrument is no response",
			message:    `{"method":"subscribe","code":0,"result":{"data":[{"a":null,"i":"USDT_USD"}]}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{"USDT_USD": types.ErrorNoResponse},
		},
		{
			name:       "unparseable price is unresolved",
			message:    `{"method":"subscribe","code":0,"result":{"data":[{"a":"x","i":"USDT_USD"}]}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{"USDT_USD": types.ErrorFailedToParsePrice},
		},
		{
			name:    "unsubscribed instrument is skipped",
			message: `{"method":"subscribe","code":0,"result":{"data":[{"a":"70.5","i":"LTC_USD"}]}}`,
		},
		{
			name:        "unknown method",
			message:     `{"method":"unsubscribe","code":0}`,
			errContains: `unknown method "unsubscribe"`,
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDT_USD")

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)
			require.Len(t, resp.Resolved, len(tt.wantResolved))
			require.Len(t, resp.Unresolved, len(tt.wantErrors))

			for ticker, wantPrice := range tt.wantResolved {
				result, ok := resp.Resolved[ticker]
				require.True(t, ok)
				require.Equal(t, wantPrice, result.Price.Text('f', -1))
				require.False(t, result.Timestamp.IsZero())
			}
			for ticker, wantCode := range tt.wantErrors {
				require.Equal(t, wantCode, resp.Unresolved[ticker].Code())
			}
		})
	}
}

func TestCopyResetsState(t *testing.T) {
	handler := newSubscribedHandler(t, "USDT_USD")

	fresh := handler.Copy().(*Handler)

	resp, _, err := fresh.HandleMessage([]byte(`{"method":"subscribe","code":0,"result":{"data":[{"a":"1","i":"USDT_USD"}]}}`))
	require.NoError(t, err)
	require.True(t, resp.Empty())

	messages, err := fresh.CreateMessages([]types.Ticker{"USDT_USD"})
	require.NoError(t, err)
	var request SubscribeRequestMessage
	require.NoError(t, json.Unmarshal(messages[0], &request))
	require.Equal(t, int64(1), request.ID)
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	handler, err := NewHandler(log.NewNopLogger(), DefaultWebSocketConfig)
	require.NoError(t, err)
	return handler.(*Handler)
}

func newSubscribedHandler(t *testing.T, tickers ...types.Ticker) *Handler {
	t.Helper()

	handler := newTestHandler(t)
	_, err := handler.CreateMessages(tickers)
	require.NoError(t, err)
	return handler
}
