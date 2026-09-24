package bybit_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/bybit"
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
				cfg.MaxSubscriptionsPerBatch = 0
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

// TestCreateMessagesBatchesTopics pins the batch limit: Bybit refuses a
// subscribe request carrying more than ten args.
func TestCreateMessagesBatchesTopics(t *testing.T) {
	cfg := DefaultWebSocketConfig
	cfg.MaxSubscriptionsPerBatch = 2
	handler, err := NewHandler(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	messages, err := handler.CreateMessages([]types.Ticker{"USDCUSDT", "BTCUSDT", "ETHUSDT"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	want := [][]string{
		{"tickers.USDCUSDT", "tickers.BTCUSDT"},
		{"tickers.ETHUSDT"},
	}
	for i, message := range messages {
		var request SubscribeRequest
		require.NoError(t, json.Unmarshal(message, &request))
		require.Equal(t, "subscribe", request.Op)
		require.Equal(t, want[i], request.Args)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no topics to subscribe to")
}

func TestHeartBeatMessages(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Len(t, messages, 1)

	var request BaseRequest
	require.NoError(t, json.Unmarshal(messages[0], &request))
	require.Equal(t, "ping", request.Op)
	require.NotEmpty(t, request.ReqID)
	require.NotZero(t, DefaultWebSocketConfig.PingInterval)
}

func TestHandleMessage(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		wantPrice   string
		wantErrCode types.ErrorCode
		errContains string
	}{
		{
			name:      "ticker update resolves the last price",
			message:   `{"topic":"tickers.USDCUSDT","ts":1673853746003,"type":"snapshot","data":{"symbol":"USDCUSDT","lastPrice":"0.9998"}}`,
			wantPrice: "0.9998",
		},
		{
			name:    "successful subscription is ignored",
			message: `{"success":true,"ret_msg":"subscribe","conn_id":"abc","req_id":"1","op":"subscribe"}`,
		},
		{
			name:        "failed subscription carries the venue message",
			message:     `{"success":false,"ret_msg":"Invalid symbol :[tickers.NOPE]","conn_id":"abc","op":"subscribe"}`,
			errContains: "subscription failed: Invalid symbol",
		},
		{
			name:    "pong is ignored",
			message: `{"success":true,"ret_msg":"pong","conn_id":"abc","op":"ping"}`,
		},
		{
			name:        "unparseable price is unresolved",
			message:     `{"topic":"tickers.USDCUSDT","data":{"symbol":"USDCUSDT","lastPrice":"x"}}`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			name:        "unsubscribed symbol is refused",
			message:     `{"topic":"tickers.LTCUSDT","data":{"symbol":"LTCUSDT","lastPrice":"70.5"}}`,
			errContains: "unsupported market LTCUSDT",
		},
		{
			name:        "update on another topic is refused",
			message:     `{"topic":"orderbook.1.USDCUSDT","data":{"symbol":"USDCUSDT"}}`,
			errContains: `invalid topic "orderbook.1.USDCUSDT"`,
		},
		{
			name:        "unknown op",
			message:     `{"op":"unsubscribe","success":true}`,
			errContains: `unknown op "unsubscribe"`,
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDCUSDT")

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["USDCUSDT"].Code())
				return
			}
			if tt.wantPrice == "" {
				require.True(t, resp.Empty())
				return
			}

			result, ok := resp.Resolved["USDCUSDT"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

func TestCopyResetsCache(t *testing.T) {
	handler := newSubscribedHandler(t, "USDCUSDT")

	fresh := handler.Copy()

	_, _, err := fresh.HandleMessage([]byte(`{"topic":"tickers.USDCUSDT","data":{"symbol":"USDCUSDT","lastPrice":"1"}}`))
	require.ErrorContains(t, err, "unsupported market USDCUSDT")
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
