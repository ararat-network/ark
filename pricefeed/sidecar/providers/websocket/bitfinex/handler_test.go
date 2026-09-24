package bitfinex_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/bitfinex"
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

func TestCreateMessages(t *testing.T) {
	handler := newTestHandler(t)

	messages, err := handler.CreateMessages([]types.Ticker{"USTUSD", "BTCUSD"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	for i, symbol := range []string{"USTUSD", "BTCUSD"} {
		var request SubscribeMessage
		require.NoError(t, json.Unmarshal(messages[i], &request))
		require.Equal(t, SubscribeMessage{Event: "subscribe", Channel: "ticker", Symbol: symbol}, request)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no tickers to subscribe to")
}

func TestHeartBeatMessagesAreNil(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Nil(t, messages)
}

func TestHandleMessageObjects(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		errContains string
	}{
		{
			name:    "info greeting is ignored",
			message: `{"event":"info","version":2,"platform":{"status":1}}`,
		},
		{
			name:    "subscribed confirmation by pair",
			message: `{"event":"subscribed","channel":"ticker","chanId":7,"symbol":"tUSTUSD","pair":"USTUSD"}`,
		},
		{
			name:        "subscribed confirmation for an unknown pair",
			message:     `{"event":"subscribed","channel":"ticker","chanId":7,"symbol":"tLTCUSD","pair":"LTCUSD"}`,
			errContains: "subscribed to unknown pair LTCUSD",
		},
		{
			name:        "error carries the venue code",
			message:     `{"event":"error","msg":"symbol: invalid","code":10001}`,
			errContains: "bitfinex error 10001 (symbol: invalid): unknown pair",
		},
		{
			name:        "unknown event",
			message:     `{"event":"conf","status":"OK"}`,
			errContains: `unknown event "conf"`,
		},
		{
			name:        "malformed json",
			message:     `{`,
			errContains: "failed to unmarshal message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USTUSD")

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)
			require.True(t, resp.Empty())
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestSubscribedMatchesThePrefixedSymbol covers a configuration that carries
// Bitfinex's t prefix: the confirmation's pair field lacks it, so the symbol
// field is the fallback match.
func TestSubscribedMatchesThePrefixedSymbol(t *testing.T) {
	handler := newSubscribedHandler(t, "tUSTUSD")

	_, _, err := handler.HandleMessage([]byte(`{"event":"subscribed","channel":"ticker","chanId":7,"symbol":"tUSTUSD","pair":"USTUSD"}`))
	require.NoError(t, err)

	resp, _, err := handler.HandleMessage([]byte(`[7,[1,2,3,4,5,6,0.9999,8,9,10]]`))
	require.NoError(t, err)
	require.Contains(t, resp.Resolved, types.Ticker("tUSTUSD"))
}

func TestHandleMessageStreams(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		wantPrice   string
		wantErrCode types.ErrorCode
		errContains string
	}{
		{
			name:      "ticker frame resolves the last price",
			message:   `[7,[0.9998,1000,0.9999,2000,0.0001,0.0001,0.99985,12345.6,1.0002,0.9990]]`,
			wantPrice: "0.99985",
		},
		{
			// Bitfinex sends more than the ten documented fields; only the
			// seventh matters, so extra trailing elements are fine.
			name:      "ticker frame with extra trailing fields resolves",
			message:   `[7,[83850,5.1,83851,3.2,-100,-0.0012,83850.5,1234.5,84000,83000,1543330803000]]`,
			wantPrice: "83850.5",
		},
		{
			name:    "heartbeat frame is ignored",
			message: `[7,"hb"]`,
		},
		{
			name:        "unknown channel id",
			message:     `[8,[1,2,3,4,5,6,7,8,9,10]]`,
			errContains: "unknown channel id 8",
		},
		{
			name:        "wrong frame length",
			message:     `[7]`,
			errContains: "invalid frame length 1",
		},
		{
			name:        "unknown marker",
			message:     `[7,"cs"]`,
			errContains: `unknown payload "cs"`,
		},
		{
			// A short payload is a venue-side surprise on a known channel,
			// so it is reported against the ticker rather than dropped.
			name:        "short payload is unresolved",
			message:     `[7,[1,2,3]]`,
			wantErrCode: types.ErrorInvalidResponse,
		},
		{
			name:        "non-numeric price is unresolved",
			message:     `[7,[1,2,3,4,5,6,"x",8,9,10]]`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USTUSD")
			_, _, err := handler.HandleMessage([]byte(`{"event":"subscribed","channel":"ticker","chanId":7,"symbol":"tUSTUSD","pair":"USTUSD"}`))
			require.NoError(t, err)

			resp, toSend, err := handler.HandleMessage([]byte(tt.message))
			require.Nil(t, toSend)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}
			require.NoError(t, err)

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["USTUSD"].Code())
				return
			}
			if tt.wantPrice == "" {
				require.True(t, resp.Empty())
				return
			}

			result, ok := resp.Resolved["USTUSD"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

// TestCopyResetsChannels pins the per-connection state: a channel id from an
// old session must not resolve prices on a new one.
func TestCopyResetsChannels(t *testing.T) {
	handler := newSubscribedHandler(t, "USTUSD")
	_, _, err := handler.HandleMessage([]byte(`{"event":"subscribed","channel":"ticker","chanId":7,"symbol":"tUSTUSD","pair":"USTUSD"}`))
	require.NoError(t, err)

	fresh := handler.Copy()

	_, _, err = fresh.HandleMessage([]byte(`[7,"hb"]`))
	require.ErrorContains(t, err, "unknown channel id 7")
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
