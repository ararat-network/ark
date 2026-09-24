package bitstamp_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/bitstamp"
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
				cfg.ReadTimeout = 0
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

	messages, err := handler.CreateMessages([]types.Ticker{"usdtusd", "btcusd"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	for i, channel := range []string{"live_trades_usdtusd", "live_trades_btcusd"} {
		var request SubscribeRequestMessage
		require.NoError(t, json.Unmarshal(messages[i], &request))
		require.Equal(t, "bts:subscribe", request.Event)
		require.Equal(t, channel, request.Data.Channel)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no tickers to subscribe to")
}

// TestHeartBeatMessages pins the client heartbeat: Bitstamp closes an idle
// connection, so the fetcher's ping interval must carry this event.
func TestHeartBeatMessages(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, `{"event":"bts:heartbeat"}`, string(messages[0]))
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
			name:      "trade resolves the price string",
			message:   `{"event":"trade","channel":"live_trades_usdtusd","data":{"id":1,"price":0.9999,"price_str":"0.99990","type":0}}`,
			wantPrice: "0.9999",
		},
		{
			name:    "heartbeat echo is ignored",
			message: `{"event":"bts:heartbeat","channel":"","data":{"status":"success"}}`,
		},
		{
			name:    "subscription succeeded is ignored",
			message: `{"event":"bts:subscription_succeeded","channel":"live_trades_usdtusd","data":{}}`,
		},
		{
			// The server closes shortly after this warning; the fetcher's
			// reconnect on the failed read is the recovery.
			name:    "reconnect request is ignored",
			message: `{"event":"bts:request_reconnect","channel":"","data":""}`,
		},
		{
			name:        "error carries the venue message",
			message:     `{"event":"bts:error","channel":"","data":{"code":4009,"message":"Incorrect data received."}}`,
			errContains: "bitstamp error 4009: Incorrect data received.",
		},
		{
			name:        "unparseable price is unresolved",
			message:     `{"event":"trade","channel":"live_trades_usdtusd","data":{"price_str":"x"}}`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			name:        "unsubscribed market is refused",
			message:     `{"event":"trade","channel":"live_trades_ltcusd","data":{"price_str":"70.5"}}`,
			errContains: "unsupported market ltcusd",
		},
		{
			name:        "trade on another channel type is refused",
			message:     `{"event":"trade","channel":"order_book_usdtusd","data":{"price_str":"1"}}`,
			errContains: `invalid trade channel "order_book_usdtusd"`,
		},
		{
			name:        "unknown event",
			message:     `{"event":"bts:unsubscribe"}`,
			errContains: `unknown event "bts:unsubscribe"`,
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "usdtusd")

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["usdtusd"].Code())
				return
			}
			if tt.wantPrice == "" {
				require.True(t, resp.Empty())
				return
			}

			result, ok := resp.Resolved["usdtusd"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

func TestCopyResetsCache(t *testing.T) {
	handler := newSubscribedHandler(t, "usdtusd")

	fresh := handler.Copy()

	_, _, err := fresh.HandleMessage([]byte(`{"event":"trade","channel":"live_trades_usdtusd","data":{"price_str":"1"}}`))
	require.ErrorContains(t, err, "unsupported market usdtusd")
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
