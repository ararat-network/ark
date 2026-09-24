package okx_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/okx"
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
				cfg.MaxTickersPerConnection = -1
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

func TestCreateMessagesBatchesTopics(t *testing.T) {
	cfg := DefaultWebSocketConfig
	cfg.MaxSubscriptionsPerBatch = 2
	handler, err := NewHandler(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	messages, err := handler.CreateMessages([]types.Ticker{"USDC-USDT", "BTC-USDT", "ETH-USDT"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	want := [][]SubscriptionTopic{
		{{Channel: "tickers", InstrumentID: "USDC-USDT"}, {Channel: "tickers", InstrumentID: "BTC-USDT"}},
		{{Channel: "tickers", InstrumentID: "ETH-USDT"}},
	}
	for i, message := range messages {
		var request SubscribeRequestMessage
		require.NoError(t, json.Unmarshal(message, &request))
		require.Equal(t, "subscribe", request.Operation)
		require.Equal(t, want[i], request.Arguments)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no topics to subscribe to")
}

// TestHeartBeatMessages pins the plain-text heartbeat: OKX closes a
// connection idle for 30 seconds, and its ping is the bare word, not JSON.
func TestHeartBeatMessages(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte("ping")}, messages)
	require.NotZero(t, DefaultWebSocketConfig.PingInterval)
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
			name:         "ticker push resolves the last price",
			message:      `{"arg":{"channel":"tickers","instId":"USDC-USDT"},"data":[{"instType":"SPOT","instId":"USDC-USDT","last":"0.9998","lastSz":"0.1","ts":"1597026383085"}]}`,
			wantResolved: map[types.Ticker]string{"USDC-USDT": "0.9998"},
		},
		{
			name:    "pong is ignored",
			message: `pong`,
		},
		{
			name:    "subscription confirmation is ignored",
			message: `{"event":"subscribe","arg":{"channel":"tickers","instId":"USDC-USDT"},"connId":"a4d3ae55"}`,
		},
		{
			name:    "connection count is logged",
			message: `{"event":"channel-conn-count","channel":"tickers","connCount":"1","connId":"a4d3ae55"}`,
		},
		{
			name:        "connection count error names the channel",
			message:     `{"event":"channel-conn-count-error","channel":"tickers","connCount":"31","connId":"a4d3ae55"}`,
			errContains: "okx channel tickers connection limit exceeded at 31 connections",
		},
		{
			// Reported rather than retried: the venue echoes the bad request
			// every time, so a retry would loop.
			name:        "error carries the venue message",
			message:     `{"event":"error","code":"60012","msg":"Invalid request: {\"op\": \"subscribe\", \"args\":[{ \"channel\" : \"tickers\", \"instId\" : \"NOPE-USDT\"}]}","connId":"a4d3ae55"}`,
			errContains: "okx error 60012: Invalid request",
		},
		{
			name:       "unparseable price is unresolved",
			message:    `{"arg":{"channel":"tickers","instId":"USDC-USDT"},"data":[{"instId":"USDC-USDT","last":"x"}]}`,
			wantErrors: map[types.Ticker]types.ErrorCode{"USDC-USDT": types.ErrorFailedToParsePrice},
		},
		{
			name:    "unsubscribed instrument is skipped",
			message: `{"arg":{"channel":"tickers","instId":"LTC-USDT"},"data":[{"instId":"LTC-USDT","last":"70.5"}]}`,
		},
		{
			name:        "push on another channel is refused",
			message:     `{"arg":{"channel":"trades","instId":"USDC-USDT"},"data":[{"instId":"USDC-USDT","last":"1"}]}`,
			errContains: `invalid channel "trades"`,
		},
		{
			name:        "unknown event",
			message:     `{"event":"unsubscribe"}`,
			errContains: `unknown event "unsubscribe"`,
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDC-USDT")

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

func TestCopyResetsCache(t *testing.T) {
	handler := newSubscribedHandler(t, "USDC-USDT")

	fresh := handler.Copy()

	resp, _, err := fresh.HandleMessage([]byte(`{"arg":{"channel":"tickers","instId":"USDC-USDT"},"data":[{"instId":"USDC-USDT","last":"1"}]}`))
	require.NoError(t, err)
	require.True(t, resp.Empty())
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
