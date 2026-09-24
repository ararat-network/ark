package kucoin_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/kucoin"
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
				cfg.WriteInterval = -1
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

// TestDefaultConfigOutlastsThePing pins the timing relation: a read
// deadline no longer than the ping interval races the pong on a quiet
// market.
func TestDefaultConfigOutlastsThePing(t *testing.T) {
	require.NotZero(t, DefaultWebSocketConfig.PingInterval)
	require.Greater(t, DefaultWebSocketConfig.ReadTimeout, DefaultWebSocketConfig.PingInterval)
}

func TestCreateMessages(t *testing.T) {
	cfg := DefaultWebSocketConfig
	cfg.MaxSubscriptionsPerBatch = 2
	handler, err := NewHandler(log.NewNopLogger(), cfg)
	require.NoError(t, err)

	messages, err := handler.CreateMessages([]types.Ticker{"USDC-USDT", "BTC-USDT", "ETH-USDT"})
	require.NoError(t, err)
	require.Len(t, messages, 2)

	want := []string{"/market/ticker:USDC-USDT,BTC-USDT", "/market/ticker:ETH-USDT"}
	for i, message := range messages {
		var request SubscribeRequestMessage
		require.NoError(t, json.Unmarshal(message, &request))
		require.Equal(t, int64(i+1), request.ID)
		require.Equal(t, "subscribe", request.Type)
		require.Equal(t, want[i], request.Topic)
		require.False(t, request.PrivateChannel)
		require.True(t, request.Response)
	}

	_, err = handler.CreateMessages(nil)
	require.ErrorContains(t, err, "no symbols to subscribe to")
}

func TestHeartBeatMessages(t *testing.T) {
	messages, err := newTestHandler(t).HeartBeatMessages()

	require.NoError(t, err)
	require.Len(t, messages, 1)

	var ping BaseMessage
	require.NoError(t, json.Unmarshal(messages[0], &ping))
	require.Equal(t, "ping", ping.Type)
	require.NotEmpty(t, ping.ID)
}

func TestPongRefreshesOnlyObservedTickers(t *testing.T) {
	tests := []struct {
		name        string
		messages    []string
		errContains string
		wantTickers []types.Ticker
	}{
		{name: "subscription pending"},
		{
			name:     "acknowledgement without a price",
			messages: []string{`{"id":"1","type":"ack"}`},
		},
		{
			name:        "rejected subscription",
			messages:    []string{`{"id":"1","type":"error","code":404,"data":"topic not found"}`},
			errContains: "kucoin error 404 for request 1",
		},
		{
			name:     "invalid price",
			messages: []string{`{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"1","price":"x"}}`},
		},
		{
			name:     "invalid sequence",
			messages: []string{`{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"x","price":"1"}}`},
		},
		{
			name:        "only one ticker observed",
			messages:    []string{`{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"1","price":"1"}}`},
			wantTickers: []types.Ticker{"USDC-USDT"},
		},
		{
			name: "all tickers observed",
			messages: []string{
				`{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"1","price":"1"}}`,
				`{"type":"message","topic":"/market/ticker:BTC-USDT","subject":"trade.ticker","data":{"sequence":"2","price":"60000"}}`,
			},
			wantTickers: []types.Ticker{"USDC-USDT", "BTC-USDT"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDC-USDT", "BTC-USDT")
			for _, message := range tt.messages {
				_, _, err := handler.HandleMessage([]byte(message))
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				} else {
					require.NoError(t, err)
				}
			}

			resp, toSend, err := handler.HandleMessage([]byte(`{"id":"1545910590801","type":"pong"}`))
			require.NoError(t, err)
			require.Nil(t, toSend)
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
			name:      "ticker resolves the price",
			message:   `{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"1545896668986","price":"0.9998","size":"0.011"}}`,
			wantPrice: "0.9998",
		},
		{
			name:    "welcome is ignored",
			message: `{"id":"hQvf8jkno","type":"welcome"}`,
		},
		{
			name:    "acknowledgement is ignored",
			message: `{"id":"1","type":"ack"}`,
		},
		{
			name:        "error carries the venue message",
			message:     `{"id":"1","type":"error","code":404,"data":"topic /market/ticker:NOPE-USDT is not found"}`,
			errContains: "kucoin error 404 for request 1: topic /market/ticker:NOPE-USDT is not found",
		},
		{
			name:        "unparseable price is unresolved",
			message:     `{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"1","price":"x"}}`,
			wantErrCode: types.ErrorFailedToParsePrice,
		},
		{
			name:        "unparseable sequence is unresolved",
			message:     `{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"x","price":"1"}}`,
			wantErrCode: types.ErrorInvalidResponse,
		},
		{
			name:        "unsubscribed symbol is refused",
			message:     `{"type":"message","topic":"/market/ticker:LTC-USDT","subject":"trade.ticker","data":{"sequence":"1","price":"70.5"}}`,
			errContains: "unsupported market LTC-USDT",
		},
		{
			name:        "data on another subject is refused",
			message:     `{"type":"message","topic":"/market/match:USDC-USDT","subject":"trade.l3match","data":{}}`,
			errContains: `invalid subject "trade.l3match"`,
		},
		{
			name:        "unknown message type",
			message:     `{"type":"unsubscribe"}`,
			errContains: `unknown message type "unsubscribe"`,
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

			if tt.wantErrCode != 0 {
				require.Empty(t, resp.Resolved)
				require.Equal(t, tt.wantErrCode, resp.Unresolved["USDC-USDT"].Code())
				return
			}
			if tt.wantPrice == "" {
				require.True(t, resp.Empty())
				return
			}

			result, ok := resp.Resolved["USDC-USDT"]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

// TestOutOfOrderTickersAreRefused pins the sequence check: a repeated or
// older sequence must not roll the price back.
func TestOutOfOrderTickersAreRefused(t *testing.T) {
	handler := newSubscribedHandler(t, "USDC-USDT")
	ticker := func(sequence, price string) []byte {
		return []byte(`{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"` + sequence + `","price":"` + price + `"}}`)
	}

	resp, _, err := handler.HandleMessage(ticker("10", "1.0001"))
	require.NoError(t, err)
	require.Contains(t, resp.Resolved, types.Ticker("USDC-USDT"))

	for _, sequence := range []string{"10", "9"} {
		resp, _, err = handler.HandleMessage(ticker(sequence, "0.9"))
		require.NoError(t, err)
		require.Empty(t, resp.Resolved)
		require.Equal(t, types.ErrorInvalidResponse, resp.Unresolved["USDC-USDT"].Code())
	}

	resp, _, err = handler.HandleMessage(ticker("11", "1.0002"))
	require.NoError(t, err)
	require.Contains(t, resp.Resolved, types.Ticker("USDC-USDT"))
}

func TestCopyResetsState(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		name := "subscription pending"
		if rejected {
			name = "subscription rejected"
		}
		t.Run(name, func(t *testing.T) {
			handler := newSubscribedHandler(t, "USDC-USDT")
			resp, _, err := handler.HandleMessage([]byte(`{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"10","price":"1"}}`))
			require.NoError(t, err)
			require.Contains(t, resp.Resolved, types.Ticker("USDC-USDT"))

			fresh := handler.Copy()
			resp, _, err = fresh.HandleMessage([]byte(`{"type":"pong"}`))
			require.NoError(t, err)
			require.True(t, resp.Empty())

			messages, err := fresh.CreateMessages([]types.Ticker{"USDC-USDT"})
			require.NoError(t, err)
			var request SubscribeRequestMessage
			require.NoError(t, json.Unmarshal(messages[0], &request))
			require.Equal(t, int64(1), request.ID)
			if rejected {
				_, _, err = fresh.HandleMessage([]byte(`{"id":"1","type":"error","code":404,"data":"topic not found"}`))
				require.ErrorContains(t, err, "kucoin error 404 for request 1")
			}

			resp, _, err = fresh.HandleMessage([]byte(`{"type":"pong"}`))
			require.NoError(t, err)
			require.True(t, resp.Empty())
			if rejected {
				return
			}

			resp, _, err = fresh.HandleMessage([]byte(`{"type":"message","topic":"/market/ticker:USDC-USDT","subject":"trade.ticker","data":{"sequence":"1","price":"1"}}`))
			require.NoError(t, err)
			require.Contains(t, resp.Resolved, types.Ticker("USDC-USDT"))
			resp, _, err = fresh.HandleMessage([]byte(`{"type":"pong"}`))
			require.NoError(t, err)
			require.True(t, resp.Resolved["USDC-USDT"].Unchanged)
		})
	}
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
