package binance_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/binance"
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
			// The registry keys handlers by name, so a config carrying another
			// provider's name would silently drive the wrong stream format.
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
				cfg.MaxBufferSize = -1
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

// TestCreateMessagesSubscribesBothStreams pins the subscription shape: Binance
// prices arrive on two streams per symbol, so a subscription that dropped one
// would halve the update rate without any error surfacing.
func TestCreateMessagesSubscribesBothStreams(t *testing.T) {
	handler := newTestHandler(t)

	messages, err := handler.CreateMessages([]types.Ticker{"BTCUSDT", "ETHUSDT"})
	require.NoError(t, err)
	// The default config batches one symbol per request, so each symbol gets
	// its own message carrying both of its streams.
	require.Len(t, messages, 2)

	want := [][]string{
		{"btcusdt@aggTrade", "btcusdt@ticker"},
		{"ethusdt@aggTrade", "ethusdt@ticker"},
	}
	for i, message := range messages {
		var request SubscribeMessageRequest
		require.NoError(t, json.Unmarshal(message, &request))
		require.Equal(t, want[i], request.Params)
	}
}

func TestCreateMessagesRejectsAnEmptyTickerSet(t *testing.T) {
	handler := newTestHandler(t)

	messages, err := handler.CreateMessages(nil)

	require.ErrorContains(t, err, "no instruments to subscribe to")
	require.Nil(t, messages)
}

// TestHeartBeatMessagesAreNil records the deliberate choice: Binance needs no
// provider-level heartbeat, so the read loop's control frames carry liveness.
func TestHeartBeatMessagesAreNil(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Nil(t, messages)
}

// TestHandleMessageSubscriptionResponses covers the correlation half of the
// protocol: a failed subscription must re-subscribe the same symbols, and a
// response nobody asked for must not be silently accepted.
func TestHandleMessageSubscriptionResponses(t *testing.T) {
	t.Run("success returns no follow-up", func(t *testing.T) {
		handler := newSubscribedHandler(t, "BTCUSDT")

		resp, toSend, err := handler.HandleMessage([]byte(`{"result":null,"id":1}`))

		require.NoError(t, err)
		require.Nil(t, toSend)
		require.Empty(t, resp.Resolved)
		require.Empty(t, resp.Unresolved)
	})

	t.Run("failure re-subscribes the same symbols", func(t *testing.T) {
		handler := newSubscribedHandler(t, "BTCUSDT")

		_, toSend, err := handler.HandleMessage([]byte(`{"result":"error","id":1}`))

		require.NoError(t, err)
		require.Len(t, toSend, 1)
		var request SubscribeMessageRequest
		require.NoError(t, json.Unmarshal(toSend[0], &request))
		require.Equal(t, []string{"btcusdt@aggTrade", "btcusdt@ticker"}, request.Params)
	})

	t.Run("unknown id is an error", func(t *testing.T) {
		handler := newSubscribedHandler(t, "BTCUSDT")

		_, _, err := handler.HandleMessage([]byte(`{"result":null,"id":99}`))

		require.ErrorContains(t, err, "failed to find instruments for message ID 99")
	})
}

// TestHandleMessagePriceUpdates is the path that feeds the oracle. Both stream
// shapes carry the price in a different field, so each needs its own case.
func TestHandleMessagePriceUpdates(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		wantPrice   string
		wantErrCode types.ErrorCode
		errContains string
	}{
		{
			name:      "ticker stream resolves the last price",
			message:   `{"stream":"btcusdt@ticker","data":{"s":"BTCUSDT","c":"67734.00000000","C":1716915868145}}`,
			wantPrice: "67734",
		},
		{
			name:      "aggregate trade stream resolves the trade price",
			message:   `{"stream":"btcusdt@aggTrade","data":{"s":"BTCUSDT","p":"67734.50000000"}}`,
			wantPrice: "67734.5",
		},
		{
			name:        "unparseable price is unresolved rather than fatal",
			message:     `{"stream":"btcusdt@ticker","data":{"s":"BTCUSDT","c":"not-a-price"}}`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			// A symbol outside the cache was never subscribed, so accepting it
			// would price a market this connection does not serve.
			name:        "unsubscribed symbol is refused",
			message:     `{"stream":"ltcbtc@ticker","data":{"s":"LTCBTC","c":"4.00000200"}}`,
			errContains: "unsupported market LTCBTC",
		},
		{
			name:        "unknown stream type",
			message:     `{"stream":"btcusdt@depth","data":{"s":"BTCUSDT"}}`,
			errContains: "unknown stream type btcusdt@depth",
		},
		{
			name:        "stream without a separator",
			message:     `{"stream":"btcusdt","data":{"s":"BTCUSDT"}}`,
			errContains: "unknown stream type btcusdt",
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "BTCUSDT")

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)

			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["BTCUSDT"].Code())
				return
			}

			result, ok := resp.Resolved["BTCUSDT"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

func TestSubscribeMessageResponseIsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		message SubscribeMessageResponse
		want    bool
	}{
		{name: "zero value", message: SubscribeMessageResponse{}, want: true},
		{name: "id set", message: SubscribeMessageResponse{ID: 1}},
		{name: "result set", message: SubscribeMessageResponse{Result: "error"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.message.IsEmpty())
		})
	}
}

func TestStreamMessageResponseGetStreamType(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		want   StreamType
	}{
		{name: "ticker", stream: "btcusdt@ticker", want: TickerStream},
		{name: "aggregate trade", stream: "btcusdt@aggTrade", want: AggregateTradeStream},
		{name: "no separator", stream: "btcusdt"},
		{name: "too many separators", stream: "btcusdt@ticker@extra"},
		{name: "empty", stream: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := StreamMessageResponse{Stream: tt.stream}
			require.Equal(t, tt.want, message.GetStreamType())
		})
	}
}

// newTestHandler returns a handler on the default config.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	handler, err := NewHandler(log.NewNopLogger(), DefaultWebSocketConfig)
	require.NoError(t, err)
	return handler.(*Handler)
}

// newSubscribedHandler returns a handler that has already subscribed to the
// given tickers, which is what populates the symbol cache every price update
// is matched against.
func newSubscribedHandler(t *testing.T, tickers ...types.Ticker) *Handler {
	t.Helper()

	handler := newTestHandler(t)
	_, err := handler.CreateMessages(tickers)
	require.NoError(t, err)
	return handler
}
