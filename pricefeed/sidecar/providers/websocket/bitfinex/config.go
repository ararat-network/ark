package bitfinex

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Bitfinex websocket provider.
	Name = "bitfinex_ws"

	// URL is the public Bitfinex websocket endpoint.
	URL = "wss://api-pub.bitfinex.com/ws/2"

	// DefaultMaxTickersPerConnection keeps each connection under Bitfinex's
	// limit of 30 subscriptions.
	DefaultMaxTickersPerConnection = 20

	// DefaultReadTimeout outlasts the heartbeat Bitfinex sends on a quiet
	// channel every 15 seconds.
	DefaultReadTimeout = 30 * time.Second
)

// DefaultWebSocketConfig is the default configuration for the Bitfinex
// websocket. Bitfinex takes one symbol per subscription message and needs
// no client heartbeat.
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
	PingInterval:             websocket.DefaultPingInterval,
	WriteInterval:            websocket.DefaultWriteInterval,
	MaxTickersPerConnection:  DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Bitfinex pair mappings. Bitfinex names
// Tether UST, and the subscription confirmation reports the pair without
// the t prefix.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USTUSD"},
}
