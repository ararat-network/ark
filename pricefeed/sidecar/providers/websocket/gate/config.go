package gate

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Gate.io websocket provider.
	Name = "gate_ws"

	// URL is the public Gate.io spot websocket endpoint.
	URL = "wss://api.gateio.ws/ws/v4/"

	// DefaultPingInterval is how often the client ping is sent.
	DefaultPingInterval = 15 * time.Second

	// DefaultReadTimeout outlasts one ping round trip on a quiet market.
	DefaultReadTimeout = 30 * time.Second
)

// DefaultWebSocketConfig is the default configuration for the Gate.io
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
	MaxTickersPerConnection:  websocket.DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Gate.io pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDC/USDT", Symbol: "USDC_USDT"},
}
