package binance

import (
	"time"

	"ark/oracle/sidecar/providers/base/websocket"
	"ark/oracle/sidecar/providers/types"
)

var (
	// Name is the name of the Binance exchange WebSocket provider.
	Name = "binance_ws"
	// WSS is the Binance websocket stream endpoint.
	WSS = "wss://stream.binance.com/stream"
	// DefaultMaxTickersPerConnection is the default maximum number of provider
	// tickers assigned to one websocket connection. Each ticker subscribes to two
	// Binance streams, so this keeps the actual stream count below Binance's limit.
	DefaultMaxTickersPerConnection = 40
	// DefaultWriteInterval is the default write interval for the Binance exchange WebSocket.
	// Binance allows up to 5 messages to be sent per second. We set this to 300ms to
	// prevent overloading the connection.
	DefaultWriteInterval = 300 * time.Millisecond
	// DefaultHandshakeTimeout is the default timeout for websocket dial and handshake.
	DefaultHandshakeTimeout = 20 * time.Second
)

// DefaultWebSocketConfig is the default configuration for the Binance exchange WebSocket.
var DefaultWebSocketConfig = websocket.Config{
	Name:                     Name,
	MaxBufferSize:            websocket.DefaultMaxBufferSize,
	ReconnectionTimeout:      websocket.DefaultReconnectionTimeout,
	PostConnectionTimeout:    websocket.DefaultPostConnectionTimeout,
	HandshakeTimeout:         DefaultHandshakeTimeout,
	Endpoints:                []types.Endpoint{{URL: WSS}},
	EnableCompression:        websocket.DefaultEnableCompression,
	ReadTimeout:              websocket.DefaultReadTimeout,
	WriteTimeout:             websocket.DefaultWriteTimeout,
	PingInterval:             websocket.DefaultPingInterval,
	WriteInterval:            DefaultWriteInterval,
	MaxTickersPerConnection:  DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Binance websocket pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDTUSD"},
}
