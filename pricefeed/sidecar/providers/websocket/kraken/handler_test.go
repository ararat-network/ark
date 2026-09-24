package kraken_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/kraken"
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
				cfg.Endpoints = []types.Endpoint{{URL: "https://ws.kraken.com/v2"}}
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

func TestCreateMessagesBatchesSymbols(t *testing.T) {
	cfg := DefaultWebSocketConfig
	cfg.MaxSubscriptionsPerBatch = 2
	handler, err := NewHandler(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	messages, err := handler.CreateMessages([]types.Ticker{"USDT/USD", "BTC/USD", "ETH/USD"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	want := [][]string{{"USDT/USD", "BTC/USD"}, {"ETH/USD"}}
	for i, message := range messages {
		var request SubscribeRequest
		require.NoError(t, json.Unmarshal(message, &request))
		require.Equal(t, "subscribe", request.Method)
		require.Equal(t, "ticker", request.Params.Channel)
		require.Equal(t, want[i], request.Params.Symbol)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no symbols to subscribe to")
}

func TestHeartBeatMessagesAreNil(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Nil(t, messages)
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
			// The price is a JSON number kept as text; it is the last trade,
			// not the day's volume-weighted average alongside it.
			name:         "ticker snapshot resolves the last trade",
			message:      `{"channel":"ticker","type":"snapshot","data":[{"symbol":"USDT/USD","bid":0.99980,"ask":0.99981,"last":0.99981,"vwap":0.99983,"timestamp":"2026-09-24T03:54:18.054886Z"}]}`,
			wantResolved: map[types.Ticker]string{"USDT/USD": "0.99981"},
		},
		{
			name:         "ticker update resolves the last trade",
			message:      `{"channel":"ticker","type":"update","data":[{"symbol":"USDT/USD","last":1.00005}]}`,
			wantResolved: map[types.Ticker]string{"USDT/USD": "1.00005"},
		},
		{
			name:    "online status is ignored",
			message: `{"channel":"status","type":"update","data":[{"version":"2.0.10","system":"online","api_version":"v2","connection_id":9471984923908980762}]}`,
		},
		{
			name:        "maintenance status is reported",
			message:     `{"channel":"status","type":"update","data":[{"system":"maintenance","api_version":"v2"}]}`,
			errContains: `kraken system status is "maintenance"`,
		},
		{
			name:        "empty status is reported",
			message:     `{"channel":"status","type":"update","data":[]}`,
			errContains: "status message carries no data",
		},
		{
			name:    "heartbeat is ignored",
			message: `{"channel":"heartbeat"}`,
		},
		{
			name:    "successful subscription is ignored",
			message: `{"method":"subscribe","result":{"channel":"ticker","event_trigger":"trades","snapshot":true,"symbol":"USDT/USD"},"success":true,"time_in":"2026-09-24T03:54:28.829165Z","time_out":"2026-09-24T03:54:28.829217Z"}`,
		},
		{
			// Reported rather than retried: the venue answers a bad symbol
			// the same way every time, so a retry would loop.
			name:        "failed subscription carries the venue message",
			message:     `{"error":"Currency pair not supported NOPE/USD","method":"subscribe","success":false,"symbol":"NOPE/USD","time_in":"2026-09-24T03:54:32.996743Z","time_out":"2026-09-24T03:54:32.996775Z"}`,
			errContains: "subscription to NOPE/USD failed: Currency pair not supported NOPE/USD",
		},
		{
			name:    "pong is ignored",
			message: `{"method":"pong","req_id":7,"time_in":"2026-09-24T03:54:32.996802Z","time_out":"2026-09-24T03:54:32.996809Z"}`,
		},
		{
			name:       "out of range price is unresolved",
			message:    `{"channel":"ticker","type":"update","data":[{"symbol":"USDT/USD","last":1e999}]}`,
			wantErrors: map[types.Ticker]types.ErrorCode{"USDT/USD": types.ErrorFailedToParsePrice},
		},
		{
			name:    "unsubscribed symbol is skipped",
			message: `{"channel":"ticker","type":"update","data":[{"symbol":"LTC/USD","last":70.5}]}`,
		},
		{
			name:        "unknown channel",
			message:     `{"channel":"trade","type":"update","data":[]}`,
			errContains: `unknown message with channel "trade" and method ""`,
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDT/USD")

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
	handler := newSubscribedHandler(t, "USDT/USD")

	fresh := handler.Copy()

	resp, _, err := fresh.HandleMessage([]byte(`{"channel":"ticker","type":"update","data":[{"symbol":"USDT/USD","last":1}]}`))
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
