package huobi

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Huobi websocket provider.
	Name = "huobi_ws"

	// URL is the public Huobi market data websocket endpoint.
	URL = "wss://api.huobi.pro/ws"

	// URLAWS is the public Huobi market data websocket endpoint hosted on
	// AWS.
	URLAWS = "wss://api-aws.huobi.pro/ws"

	// DefaultReadTimeout outlasts the ping Huobi sends every five seconds.
	DefaultReadTimeout = 20 * time.Second

	// MaxDecompressedBytes bounds one message after gzip decompression. A
	// ticker message is a few hundred bytes; the cap refuses a compressed
	// frame that inflates far past it.
	MaxDecompressedBytes = 1 << 20
)

// DefaultWebSocketConfig is the default configuration for the Huobi
// websocket. Huobi takes one topic per subscription message and drives the
// heartbeat itself, so no client heartbeat is needed.
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
	MaxTickersPerConnection:  websocket.DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in Huobi pair mappings. Symbols are
// lower-case.
var DefaultMarkets = types.Markets{
	{Pair: "USDC/USDT", Symbol: "usdcusdt"},
}
