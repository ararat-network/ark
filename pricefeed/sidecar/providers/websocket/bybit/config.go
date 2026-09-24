package bybit

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Bybit websocket provider.
	Name = "bybit_ws"

	// URL is the public Bybit spot websocket endpoint.
	URL = "wss://stream.bybit.com/v5/public/spot"

	// URLTestnet is the public Bybit spot testnet websocket endpoint.
	URLTestnet = "wss://stream-testnet.bybit.com/v5/public/spot"

	// DefaultPingInterval keeps the connection under Bybit's 20 second idle
	// limit.
	DefaultPingInterval = 15 * time.Second

	// DefaultReadTimeout outlasts one heartbeat round trip on a quiet market.
	DefaultReadTimeout = 30 * time.Second

	// DefaultMaxSubscriptionsPerBatch is Bybit's limit on args in one
	// subscribe request.
	DefaultMaxSubscriptionsPerBatch = 10
)

// DefaultWebSocketConfig is the default configuration for the Bybit
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
	MaxSubscriptionsPerBatch: DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Bybit pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDC/USDT", Symbol: "USDCUSDT"},
}
