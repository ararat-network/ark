package cryptodotcom

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Crypto.com websocket provider.
	Name = "crypto_dot_com_ws"

	// URL is the production Crypto.com market data websocket endpoint.
	URL = "wss://stream.crypto.com/exchange/v1/market"

	// URLSandbox is the sandbox Crypto.com market data websocket endpoint,
	// which serves static prices.
	URLSandbox = "wss://uat-stream.3ona.co/exchange/v1/market"

	// DefaultMaxTickersPerConnection keeps each connection under Crypto.com's
	// limit of 400 subscriptions.
	DefaultMaxTickersPerConnection = 200

	// DefaultPostConnectionTimeout is the one second pause Crypto.com asks
	// for before the first subscription.
	DefaultPostConnectionTimeout = time.Second

	// DefaultReadTimeout outlasts the heartbeat Crypto.com sends every 30
	// seconds.
	DefaultReadTimeout = 45 * time.Second
)

// DefaultWebSocketConfig is the default configuration for the Crypto.com
// websocket. The server heartbeat is answered from the read loop, so no
// client heartbeat is needed.
var DefaultWebSocketConfig = websocket.Config{
	Name:                     Name,
	MaxBufferSize:            websocket.DefaultMaxBufferSize,
	ReconnectionTimeout:      websocket.DefaultReconnectionTimeout,
	PostConnectionTimeout:    DefaultPostConnectionTimeout,
	HandshakeTimeout:         websocket.DefaultHandshakeTimeout,
	Endpoints:                []types.Endpoint{{URL: URL}},
	EnableCompression:        websocket.DefaultEnableCompression,
	ReadTimeout:              DefaultReadTimeout,
	WriteTimeout:             websocket.DefaultWriteTimeout,
	PingInterval:             websocket.DefaultPingInterval,
	WriteInterval:            websocket.DefaultWriteInterval,
	MaxTickersPerConnection:  DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Crypto.com pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDT_USD"},
}
