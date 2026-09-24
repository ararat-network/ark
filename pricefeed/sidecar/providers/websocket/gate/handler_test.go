package gate_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/gate"
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
				cfg.HandshakeTimeout = 0
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
	cfg := DefaultWebSocketConfig
	cfg.MaxSubscriptionsPerBatch = 2
	handler, err := NewHandler(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	messages, err := handler.CreateMessages([]types.Ticker{"USDC_USDT", "BTC_USDT", "ETH_USDT"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	want := [][]string{{"USDC_USDT", "BTC_USDT"}, {"ETH_USDT"}}
	for i, message := range messages {
		var request SubscribeRequest
		require.NoError(t, json.Unmarshal(message, &request))
		require.Equal(t, "spot.tickers", request.Channel)
		require.Equal(t, "subscribe", request.Event)
		require.Equal(t, int64(i+1), request.ID)
		// The time is a Unix timestamp, not the clock's seconds component.
		require.Greater(t, request.Time, int64(1_600_000_000))
		require.Equal(t, want[i], request.Payload)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no pairs to subscribe to")
}

func TestHeartBeatMessages(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Len(t, messages, 1)

	var ping BaseMessage
	require.NoError(t, json.Unmarshal(messages[0], &ping))
	require.Equal(t, "spot.ping", ping.Channel)
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
			message:   `{"time":1669107766,"channel":"spot.tickers","event":"update","result":{"currency_pair":"USDC_USDT","last":"0.9998","lowest_ask":"0.9999"}}`,
			wantPrice: "0.9998",
		},
		{
			name:    "successful subscription is ignored",
			message: `{"time":1611541000,"channel":"spot.tickers","event":"subscribe","error":null,"result":{"status":"success"}}`,
		},
		{
			name:    "pong is ignored",
			message: `{"time":1545404023,"channel":"spot.pong","event":"","result":null}`,
		},
		{
			name:        "subscription error carries the venue message",
			message:     `{"time":1611541000,"channel":"spot.tickers","event":"subscribe","error":{"code":2,"message":"invalid argument"},"result":null}`,
			errContains: "gate error 2 (invalid argument): invalid argument in request",
		},
		{
			name:        "subscription without success status",
			message:     `{"channel":"spot.tickers","event":"subscribe","result":{"status":"fail"}}`,
			errContains: `subscription was not successful: "fail"`,
		},
		{
			name:        "unparseable price is unresolved",
			message:     `{"channel":"spot.tickers","event":"update","result":{"currency_pair":"USDC_USDT","last":"x"}}`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			name:        "unsubscribed pair is refused",
			message:     `{"channel":"spot.tickers","event":"update","result":{"currency_pair":"LTC_USDT","last":"70.5"}}`,
			errContains: "unsupported market LTC_USDT",
		},
		{
			name:        "update on another channel is refused",
			message:     `{"channel":"spot.trades","event":"update","result":{"currency_pair":"USDC_USDT"}}`,
			errContains: `invalid channel "spot.trades"`,
		},
		{
			name:        "unknown event",
			message:     `{"channel":"spot.tickers","event":"unsubscribe"}`,
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
			handler := newSubscribedHandler(t, "USDC_USDT")

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["USDC_USDT"].Code())
				return
			}
			if tt.wantPrice == "" {
				require.True(t, resp.Empty())
				return
			}

			result, ok := resp.Resolved["USDC_USDT"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

func TestCopyResetsState(t *testing.T) {
	handler := newSubscribedHandler(t, "USDC_USDT")

	fresh := handler.Copy().(*Handler)

	_, _, err := fresh.HandleMessage([]byte(`{"channel":"spot.tickers","event":"update","result":{"currency_pair":"USDC_USDT","last":"1"}}`))
	require.ErrorContains(t, err, "unsupported market USDC_USDT")

	messages, err := fresh.CreateMessages([]types.Ticker{"USDC_USDT"})
	require.NoError(t, err)
	var request SubscribeRequest
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
