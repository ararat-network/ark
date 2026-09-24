package mexc_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/mexc"
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
				cfg.PingInterval = -1
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

	messages, err := handler.CreateMessages([]types.Ticker{"USDCUSDT", "btcusdt"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	// The stream name upper-cases the symbol whatever the configured case.
	want := []string{
		"spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8",
		"spot@public.miniTicker.v3.api.pb@BTCUSDT@UTC+8",
	}
	for i, message := range messages {
		var request RequestMessage
		require.NoError(t, json.Unmarshal(message, &request))
		require.Equal(t, "SUBSCRIPTION", request.Method)
		require.Equal(t, []string{want[i]}, request.Params)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no streams to subscribe to")
}

// TestCreateMessagesRefusesAnOversizedShard pins the venue limit as a
// handler-side check, independent of the fetcher's sharding.
func TestCreateMessagesRefusesAnOversizedShard(t *testing.T) {
	cfg := DefaultWebSocketConfig
	cfg.MaxTickersPerConnection = 2
	handler, err := NewHandler(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	_, err = handler.CreateMessages([]types.Ticker{"A", "B", "C"})
	require.ErrorContains(t, err, "cannot subscribe to 3 tickers on one connection; the limit is 2")
}

func TestHeartBeatMessages(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.JSONEq(t, `{"method":"PING"}`, string(messages[0]))
	require.NotZero(t, DefaultWebSocketConfig.PingInterval)
}

func TestHandleMessage(t *testing.T) {
	tests := []struct {
		name        string
		message     []byte
		wantPrice   string
		wantErrCode types.ErrorCode
		errContains string
	}{
		{
			name:      "live frame resolves the price",
			message:   []byte(liveFrame),
			wantPrice: "1.0001",
		},
		{
			name:      "ticker push resolves the price",
			message:   pushFrame("spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8", "USDCUSDT", miniTickerBody("USDCUSDT", "0.9998")),
			wantPrice: "0.9998",
		},
		{
			// The wrapper's symbol stands in when the body omits its own.
			name:      "ticker push without a body symbol uses the wrapper symbol",
			message:   pushFrame("spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8", "USDCUSDT", appendString(nil, 2, "0.9997")),
			wantPrice: "0.9997",
		},
		{
			name:    "subscription echo is ignored",
			message: []byte(`{"id":0,"code":0,"msg":"spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8"}`),
		},
		{
			name:    "pong is ignored",
			message: []byte(`{"id":0,"code":0,"msg":"PONG"}`),
		},
		{
			name:        "error code carries the venue message",
			message:     []byte(`{"id":0,"code":1,"msg":"Blocked!"}`),
			errContains: "mexc error 1: Blocked!",
		},
		{
			// A refused subscription arrives with a zero code, so the text
			// is the only signal.
			name:        "refused subscription is reported",
			message:     []byte(`{"id":0,"code":0,"msg":"Not Subscribed successfully! [spot@public.miniTicker.v3.api.pb@NOPEUSDT@UTC+8].  Reason： Blocked! "}`),
			errContains: `mexc refused: "Not Subscribed successfully!`,
		},
		{
			name:        "unparseable price is unresolved",
			message:     pushFrame("spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8", "USDCUSDT", miniTickerBody("USDCUSDT", "x")),
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			name:        "unsubscribed symbol is refused",
			message:     pushFrame("spot@public.miniTicker.v3.api.pb@LTCUSDT@UTC+8", "LTCUSDT", miniTickerBody("LTCUSDT", "70.5")),
			errContains: "unsupported market LTCUSDT",
		},
		{
			name:        "push on another channel is refused",
			message:     pushFrame("spot@public.aggre.deals.v3.api.pb@100ms@USDCUSDT", "USDCUSDT", nil),
			errContains: `invalid channel "spot@public.aggre.deals.v3.api.pb@100ms@USDCUSDT"`,
		},
		{
			name:        "mini ticker channel without a mini ticker body",
			message:     appendString(nil, 1, "spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8"),
			errContains: "push frame carries no mini ticker",
		},
		{
			name:        "truncated frame",
			message:     []byte{0x0a, 0x10, 'a'},
			errContains: "decoding push frame",
		},
		{
			name:        "empty message",
			message:     nil,
			errContains: "empty message",
		},
		{
			name:        "malformed json",
			message:     []byte(`{`),
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDCUSDT")

			resp, toSend, err := handler.HandleMessage(tt.message)
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

	_, _, err := fresh.HandleMessage(pushFrame("spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8", "USDCUSDT", miniTickerBody("USDCUSDT", "1")))
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
