package huobi_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/huobi"
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
				cfg.ReconnectionTimeout = 0
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

	messages, err := handler.CreateMessages([]types.Ticker{"usdcusdt", "btcusdt"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	for i, symbol := range []string{"usdcusdt", "btcusdt"} {
		var request SubscriptionRequest
		require.NoError(t, json.Unmarshal(messages[i], &request))
		require.Equal(t, SubscriptionRequest{Sub: "market." + symbol + ".ticker", ID: symbol}, request)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no tickers to subscribe to")
}

func TestHeartBeatMessagesAreNil(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Nil(t, messages)
}

func TestServerPingRefreshesOnlyObservedTickers(t *testing.T) {
	tests := []struct {
		name        string
		messages    []string
		errContains string
		wantTickers []types.Ticker
	}{
		{name: "subscription pending"},
		{
			name:     "acknowledgement without a price",
			messages: []string{`{"id":"usdcusdt","status":"ok","subbed":"market.usdcusdt.ticker"}`},
		},
		{
			name:        "rejected subscription",
			messages:    []string{`{"id":"usdcusdt","status":"error","err-msg":"invalid topic"}`},
			errContains: "subscription usdcusdt failed",
		},
		{
			name:     "invalid price",
			messages: []string{`{"ch":"market.usdcusdt.ticker","tick":{"lastPrice":1e999}}`},
		},
		{
			name:        "only one ticker observed",
			messages:    []string{`{"ch":"market.usdcusdt.ticker","tick":{"lastPrice":1}}`},
			wantTickers: []types.Ticker{"usdcusdt"},
		},
		{
			name: "all tickers observed",
			messages: []string{
				`{"ch":"market.usdcusdt.ticker","tick":{"lastPrice":1}}`,
				`{"ch":"market.btcusdt.ticker","tick":{"lastPrice":60000}}`,
			},
			wantTickers: []types.Ticker{"usdcusdt", "btcusdt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "usdcusdt", "btcusdt")
			for _, message := range tt.messages {
				_, _, err := handler.HandleMessage(gz(t, message))
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				} else {
					require.NoError(t, err)
				}
			}

			resp, toSend, err := handler.HandleMessage(gz(t, `{"ping":1492420473027}`))
			require.NoError(t, err)
			require.Len(t, toSend, 1)
			require.JSONEq(t, `{"pong":1492420473027}`, string(toSend[0]))
			require.Empty(t, resp.Unresolved)
			require.Len(t, resp.Resolved, len(tt.wantTickers))
			for _, ticker := range tt.wantTickers {
				require.True(t, resp.Resolved[ticker].Unchanged)
				require.False(t, resp.Resolved[ticker].Timestamp.IsZero())
			}
		})
	}
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
			name:      "ticker stream resolves the last price",
			message:   `{"ch":"market.usdcusdt.ticker","ts":1630982370526,"tick":{"open":1.0,"lastPrice":0.99985,"lastSize":10}}`,
			wantPrice: "0.99985",
		},
		{
			name:    "successful subscription is ignored",
			message: `{"id":"usdcusdt","status":"ok","subbed":"market.usdcusdt.ticker","ts":1}`,
		},
		{
			name:        "failed subscription carries the venue message",
			message:     `{"status":"error","id":"nope","err-code":"bad-request","err-msg":"invalid topic market.nope.ticker","ts":1}`,
			errContains: `subscription nope failed with status "error": invalid topic market.nope.ticker`,
		},
		{
			name:        "subscription to an unexpected topic",
			message:     `{"id":"usdcusdt","status":"ok","subbed":"market.usdcusdt.trade.detail","ts":1}`,
			errContains: `unexpected topic "market.usdcusdt.trade.detail"`,
		},
		{
			// Huobi frames prices as JSON numbers, so the parser's own refusal
			// is a magnitude the oracle cannot carry.
			name:        "out of range price is unresolved",
			message:     `{"ch":"market.usdcusdt.ticker","tick":{"lastPrice":1e999}}`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			name:        "non-numeric price is a decode failure",
			message:     `{"ch":"market.usdcusdt.ticker","tick":{"lastPrice":"x"}}`,
			errContains: "failed to unmarshal ticker stream",
		},
		{
			name:        "unsubscribed symbol is refused",
			message:     `{"ch":"market.ltcusdt.ticker","tick":{"lastPrice":70.5}}`,
			errContains: "unsupported market ltcusdt",
		},
		{
			name:        "stream on another channel shape is refused",
			message:     `{"ch":"market.usdcusdt.trade.detail","tick":{"lastPrice":1}}`,
			errContains: `invalid ticker channel "market.usdcusdt.trade.detail"`,
		},
		{
			name:        "unknown message",
			message:     `{"rep":"market.usdcusdt.kline.1min"}`,
			errContains: "unknown message",
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "usdcusdt")

			resp, toSend, err := handler.HandleMessage(gz(t, tt.message))
			require.Nil(t, toSend)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["usdcusdt"].Code())
				return
			}
			if tt.wantPrice == "" {
				require.True(t, resp.Empty())
				return
			}

			result, ok := resp.Resolved["usdcusdt"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

// TestDecompressionIsBounded pins the untrusted-bytes bound: a frame that
// is not gzip is refused, and one that inflates past the cap is refused
// before it is parsed.
func TestDecompressionIsBounded(t *testing.T) {
	handler := newSubscribedHandler(t, "usdcusdt")

	_, _, err := handler.HandleMessage([]byte(`{"ping":1}`))
	require.ErrorContains(t, err, "failed to open gzip message")

	bomb := gz(t, strings.Repeat(" ", MaxDecompressedBytes+1)+`{"ping":1}`)
	require.Less(t, len(bomb), 16<<10)
	_, _, err = handler.HandleMessage(bomb)
	require.ErrorContains(t, err, "decompressed message exceeds")
}

func TestCopyResetsState(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		name := "subscription pending"
		if rejected {
			name = "subscription rejected"
		}
		t.Run(name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "usdcusdt")
			price := gz(t, `{"ch":"market.usdcusdt.ticker","tick":{"lastPrice":1}}`)
			resp, _, err := handler.HandleMessage(price)
			require.NoError(t, err)
			require.Contains(t, resp.Resolved, types.Ticker("usdcusdt"))

			fresh := handler.Copy()
			resp, _, err = fresh.HandleMessage(gz(t, `{"ping":1}`))
			require.NoError(t, err)
			require.True(t, resp.Empty())

			_, err = fresh.CreateMessages([]types.Ticker{"usdcusdt"})
			require.NoError(t, err)
			if rejected {
				_, _, err = fresh.HandleMessage(gz(t, `{"id":"usdcusdt","status":"error","err-msg":"invalid topic"}`))
				require.ErrorContains(t, err, "subscription usdcusdt failed")
			}

			resp, _, err = fresh.HandleMessage(gz(t, `{"ping":2}`))
			require.NoError(t, err)
			require.True(t, resp.Empty())
			if rejected {
				return
			}

			resp, _, err = fresh.HandleMessage(price)
			require.NoError(t, err)
			require.Contains(t, resp.Resolved, types.Ticker("usdcusdt"))
			resp, _, err = fresh.HandleMessage(gz(t, `{"ping":3}`))
			require.NoError(t, err)
			require.True(t, resp.Resolved["usdcusdt"].Unchanged)
		})
	}
}

// gz gzip-compresses s the way Huobi frames every message.
func gz(t *testing.T, s string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write([]byte(s))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes()
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
