package coinbase

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Coinbase websocket provider.
	Name = "coinbase_ws"

	// URL is the production Coinbase Exchange websocket feed.
	URL = "wss://ws-feed.exchange.coinbase.com"

	// URLSandbox is the sandbox Coinbase Exchange websocket feed.
	URLSandbox = "wss://ws-feed-public.sandbox.exchange.coinbase.com"

	// DefaultWriteTimeout is the write timeout Coinbase recommends.
	DefaultWriteTimeout = 5 * time.Second
)

// DefaultWebSocketConfig is the default configuration for the Coinbase
// websocket. The heartbeat channel keeps a quiet market's reads flowing, so
// no client heartbeat is needed.
var DefaultWebSocketConfig = websocket.Config{
	Name:                     Name,
	MaxBufferSize:            websocket.DefaultMaxBufferSize,
	ReconnectionTimeout:      websocket.DefaultReconnectionTimeout,
	PostConnectionTimeout:    websocket.DefaultPostConnectionTimeout,
	HandshakeTimeout:         websocket.DefaultHandshakeTimeout,
	Endpoints:                []types.Endpoint{{URL: URL}},
	EnableCompression:        websocket.DefaultEnableCompression,
	ReadTimeout:              websocket.DefaultReadTimeout,
	WriteTimeout:             DefaultWriteTimeout,
	PingInterval:             websocket.DefaultPingInterval,
	WriteInterval:            websocket.DefaultWriteInterval,
	MaxTickersPerConnection:  websocket.DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Coinbase websocket pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDT-USD"},
}
