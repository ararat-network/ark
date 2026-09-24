package kraken

import (
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Kraken websocket provider.
	Name = "kraken_ws"

	// URL is the public Kraken websocket v2 endpoint.
	URL = "wss://ws.kraken.com/v2"

	// URLBeta is the public Kraken websocket v2 beta endpoint.
	URLBeta = "wss://beta-ws.kraken.com/v2"
)

// DefaultWebSocketConfig is the default configuration for the Kraken
// websocket. Kraken sends a heartbeat every second on a quiet connection,
// so no client heartbeat is needed. Several symbols may share one subscribe
// request; the default keeps one per request.
var DefaultWebSocketConfig = websocket.Config{
	Name:                     Name,
	MaxBufferSize:            websocket.DefaultMaxBufferSize,
	ReconnectionTimeout:      websocket.DefaultReconnectionTimeout,
	PostConnectionTimeout:    websocket.DefaultPostConnectionTimeout,
	HandshakeTimeout:         websocket.DefaultHandshakeTimeout,
	Endpoints:                []types.Endpoint{{URL: URL}},
	EnableCompression:        websocket.DefaultEnableCompression,
	ReadTimeout:              websocket.DefaultReadTimeout,
	WriteTimeout:             websocket.DefaultWriteTimeout,
	PingInterval:             websocket.DefaultPingInterval,
	WriteInterval:            websocket.DefaultWriteInterval,
	MaxTickersPerConnection:  websocket.DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Kraken websocket pair mappings. The
// symbol is Kraken's websocket pair name.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDT/USD"},
}
