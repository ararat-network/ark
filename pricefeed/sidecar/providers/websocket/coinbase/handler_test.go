package coinbase_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/coinbase"
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
				cfg.WriteTimeout = 0
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

// TestCreateMessagesSubscribesBothChannels pins the subscription shape: the
// heartbeat channel is what lets a quiet market confirm its price.
func TestCreateMessagesSubscribesBothChannels(t *testing.T) {
	handler := newTestHandler(t)

	messages, err := handler.CreateMessages([]types.Ticker{"USDT-USD", "BTC-USD"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	for i, product := range []string{"USDT-USD", "BTC-USD"} {
		var request SubscribeRequestMessage
		require.NoError(t, json.Unmarshal(messages[i], &request))
		require.Equal(t, "subscribe", request.Type)
		require.Equal(t, []string{product}, request.ProductIDs)
		require.Equal(t, []string{"ticker", "heartbeat"}, request.Channels)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no products to subscribe to")
}

func TestHeartBeatMessagesAreNil(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Nil(t, messages)
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
			name:      "ticker resolves the price",
			message:   `{"type":"ticker","sequence":10,"product_id":"USDT-USD","price":"1.0001","trade_id":5,"time":"2026-10-11T08:00:00.123456Z"}`,
			wantPrice: "1.0001",
		},
		{
			name:    "subscriptions listing is ignored",
			message: `{"type":"subscriptions","channels":[{"name":"ticker","product_ids":["USDT-USD"]}]}`,
		},
		{
			name:        "error carries the venue message",
			message:     `{"type":"error","message":"Failed to subscribe","reason":"NOPE-USD is not a valid product"}`,
			errContains: "coinbase error: Failed to subscribe: NOPE-USD is not a valid product",
		},
		{
			// A match without its trade time cannot be aged, so it fails closed.
			name:        "missing match time is unresolved",
			message:     `{"type":"ticker","sequence":10,"product_id":"USDT-USD","price":"1.0001","trade_id":5}`,
			wantErrCode: types.ErrorInvalidResponse,
		},
		{
			name:        "unparseable price is unresolved",
			message:     `{"type":"ticker","sequence":10,"product_id":"USDT-USD","price":"x","trade_id":5,"time":"2026-10-11T08:00:00.123456Z"}`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			// No match has been seen on a fresh session, so a heartbeat
			// cannot confirm a price.
			name:        "heartbeat before any match has nothing to confirm",
			message:     `{"type":"heartbeat","sequence":10,"last_trade_id":5,"product_id":"USDT-USD"}`,
			wantErrCode: types.ErrorNoExistingPrice,
		},
		{
			name:        "unsubscribed product is refused",
			message:     `{"type":"ticker","sequence":10,"product_id":"LTC-USD","price":"70.5","trade_id":5,"time":"2026-10-11T08:00:00.123456Z"}`,
			errContains: "unsupported market LTC-USD",
		},
		{
			name:        "unknown message type",
			message:     `{"type":"l2update"}`,
			errContains: `unknown message type "l2update"`,
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDT-USD")

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["USDT-USD"].Code())
				return
			}
			if tt.wantPrice == "" {
				require.True(t, resp.Empty())
				return
			}

			result, ok := resp.Resolved["USDT-USD"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

// TestTickerIsObservedWhenItTraded pins that a match counts as observed when
// it traded, not when it arrived: the ticker Coinbase sends on subscribe
// repeats the last match, however old, and must age from its trade. A venue
// clock ahead of this one never dates a match after its arrival.
func TestTickerIsObservedWhenItTraded(t *testing.T) {
	tests := []struct {
		name   string
		traded time.Time
		future bool
	}{
		{name: "old match on subscribe", traded: time.Now().UTC().Add(-40 * time.Minute)},
		{name: "venue clock ahead", traded: time.Now().UTC().Add(time.Hour), future: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDT-USD")
			message := `{"type":"ticker","sequence":10,"product_id":"USDT-USD","price":"1.0001","trade_id":5,"time":"` +
				tt.traded.Format(time.RFC3339Nano) + `"}`

			resp, _, err := handler.HandleMessage([]byte(message))
			require.NoError(t, err)
			result, ok := resp.Resolved["USDT-USD"]
			require.True(t, ok)
			require.WithinDuration(t, time.Now(), result.Timestamp, time.Minute)
			if tt.future {
				require.True(t, result.LastObserved.Equal(result.Timestamp))
				return
			}
			require.True(t, result.LastObserved.Equal(tt.traded))
		})
	}
}

// TestHeartbeatConfirmsTheLastMatch covers the unchanged path: a heartbeat
// naming the last seen trade confirms the price; one naming another trade
// means a match was missed.
func TestHeartbeatConfirmsTheLastMatch(t *testing.T) {
	handler := newSubscribedHandler(t, "USDT-USD")

	_, _, err := handler.HandleMessage([]byte(`{"type":"ticker","sequence":10,"product_id":"USDT-USD","price":"1.0001","trade_id":5,"time":"2026-10-11T08:00:00.123456Z"}`))
	require.NoError(t, err)

	resp, _, err := handler.HandleMessage([]byte(`{"type":"heartbeat","sequence":11,"last_trade_id":5,"product_id":"USDT-USD"}`))
	require.NoError(t, err)
	result, ok := resp.Resolved["USDT-USD"]
	require.True(t, ok)
	require.True(t, result.Unchanged)
	require.Nil(t, result.Price)

	resp, _, err = handler.HandleMessage([]byte(`{"type":"heartbeat","sequence":12,"last_trade_id":6,"product_id":"USDT-USD"}`))
	require.NoError(t, err)
	require.Empty(t, resp.Resolved)
	require.Equal(t, types.ErrorNoExistingPrice, resp.Unresolved["USDT-USD"].Code())
}

// TestOutOfOrderMessagesAreRefused pins the sequence check: a late match
// must not roll the price back, and a late heartbeat must not confirm it.
func TestOutOfOrderMessagesAreRefused(t *testing.T) {
	handler := newSubscribedHandler(t, "USDT-USD")

	_, _, err := handler.HandleMessage([]byte(`{"type":"ticker","sequence":10,"product_id":"USDT-USD","price":"1.0001","trade_id":5,"time":"2026-10-11T08:00:00.123456Z"}`))
	require.NoError(t, err)

	resp, _, err := handler.HandleMessage([]byte(`{"type":"ticker","sequence":9,"product_id":"USDT-USD","price":"0.9","trade_id":4,"time":"2026-10-11T08:00:00.123456Z"}`))
	require.NoError(t, err)
	require.Empty(t, resp.Resolved)
	require.Equal(t, types.ErrorInvalidResponse, resp.Unresolved["USDT-USD"].Code())

	resp, _, err = handler.HandleMessage([]byte(`{"type":"heartbeat","sequence":9,"last_trade_id":5,"product_id":"USDT-USD"}`))
	require.NoError(t, err)
	require.Empty(t, resp.Resolved)
	require.Equal(t, types.ErrorInvalidResponse, resp.Unresolved["USDT-USD"].Code())
}

func TestCopyResetsState(t *testing.T) {
	handler := newSubscribedHandler(t, "USDT-USD")
	_, _, err := handler.HandleMessage([]byte(`{"type":"ticker","sequence":10,"product_id":"USDT-USD","price":"1.0001","trade_id":5,"time":"2026-10-11T08:00:00.123456Z"}`))
	require.NoError(t, err)

	fresh := handler.Copy().(*Handler)
	_, err = fresh.CreateMessages([]types.Ticker{"USDT-USD"})
	require.NoError(t, err)

	resp, _, err := fresh.HandleMessage([]byte(`{"type":"heartbeat","sequence":1,"last_trade_id":5,"product_id":"USDT-USD"}`))
	require.NoError(t, err)
	require.Equal(t, types.ErrorNoExistingPrice, resp.Unresolved["USDT-USD"].Code())
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
