package mexc

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the MEXC websocket provider.
	Name = "mexc_ws"

	// URL is the public MEXC spot websocket endpoint. It serves the
	// protobuf-framed v3 streams; the older JSON endpoint refuses them.
	URL = "wss://wbs-api.mexc.com/ws"

	// DefaultPingInterval keeps under the 30 second interval MEXC documents,
	// with room for network latency.
	DefaultPingInterval = 20 * time.Second

	// DefaultReadTimeout outlasts two ping round trips on a quiet market, so
	// one lost pong does not reconnect.
	DefaultReadTimeout = 40 * time.Second

	// DefaultMaxTickersPerConnection keeps each connection under MEXC's
	// limit of 30 subscriptions.
	DefaultMaxTickersPerConnection = 20
)

// DefaultWebSocketConfig is the default configuration for the MEXC
// websocket.
var DefaultWebSocketConfig = websocket.Config{
	Name:                     Name,
	MaxBufferSize:            websocket.DefaultMaxBufferSize,
	ReconnectionTimeout:      websocket.DefaultReconnectionTimeout,
	PostConnectionTimeout:    websocket.DefaultPostConnectionTimeout,
	HandshakeTimeout:         websocket.DefaultHandshakeTimeout,
	Endpoints:                []types.Endpoint{{URL: URL}},
	EnableCompression:        websocket.DefaultEnableCompression,
	ReadTimeout:              DefaultReadTimeout,
	WriteTimeout:             websocket.DefaultWriteTimeout,
	PingInterval:             DefaultPingInterval,
	WriteInterval:            websocket.DefaultWriteInterval,
	MaxTickersPerConnection:  DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in MEXC pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDC/USDT", Symbol: "USDCUSDT"},
}
