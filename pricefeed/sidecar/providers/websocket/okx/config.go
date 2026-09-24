package okx

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the OKX websocket provider.
	Name = "okx_ws"

	// URL is the public OKX websocket endpoint.
	URL = "wss://ws.okx.com:8443/ws/v5/public"

	// URLAWS is the public OKX websocket endpoint hosted on AWS.
	URLAWS = "wss://wsaws.okx.com:8443/ws/v5/public"

	// URLDemo is the public OKX demo trading websocket endpoint.
	URLDemo = "wss://wspap.okx.com:8443/ws/v5/public?brokerId=9999"

	// DefaultPingInterval keeps under the 30 second idle limit after which
	// OKX closes a connection.
	DefaultPingInterval = 15 * time.Second

	// DefaultReadTimeout outlasts one ping round trip on a quiet market.
	DefaultReadTimeout = 30 * time.Second

	// DefaultWriteInterval spaces subscriptions under OKX's limit of three
	// messages per second.
	DefaultWriteInterval = 3 * time.Second

	// DefaultMaxTickersPerConnection bounds one connection's subscriptions.
	DefaultMaxTickersPerConnection = 50

	// DefaultMaxSubscriptionsPerBatch bounds one subscribe request's args.
	DefaultMaxSubscriptionsPerBatch = 25
)

// DefaultWebSocketConfig is the default configuration for the OKX websocket.
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
	WriteInterval:            DefaultWriteInterval,
	MaxTickersPerConnection:  DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in OKX pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDC/USDT", Symbol: "USDC-USDT"},
}
