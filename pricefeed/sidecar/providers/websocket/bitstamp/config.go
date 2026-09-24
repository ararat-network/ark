package bitstamp

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Bitstamp websocket provider.
	Name = "bitstamp_ws"

	// URL is the public Bitstamp websocket endpoint.
	URL = "wss://ws.bitstamp.net"

	// DefaultPingInterval is how often the client heartbeat is sent.
	DefaultPingInterval = 10 * time.Second

	// DefaultReadTimeout outlasts one heartbeat round trip on a quiet market.
	DefaultReadTimeout = 20 * time.Second
)

// DefaultWebSocketConfig is the default configuration for the Bitstamp
// websocket. Bitstamp takes one channel per subscription message.
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
	MaxTickersPerConnection:  websocket.DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Bitstamp websocket pair mappings. The
// symbol is the lower-case market suffix of the live trades channel.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "usdtusd"},
}
