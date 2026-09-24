package kucoin

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the KuCoin websocket provider.
	Name = "kucoin_ws"

	// URL is the public KuCoin spot websocket endpoint. Every dial carries a
	// connect token fetched from TokenURL first.
	URL = "wss://ws-api-spot.kucoin.com/"

	// TokenURL issues the public connect token. It is fixed: the token
	// response names no credential, and the endpoint the token is spent on
	// stays the configured one.
	TokenURL = "https://api.kucoin.com/api/v1/bullet-public"

	// DefaultHandshakeTimeout covers the token request and the dial.
	DefaultHandshakeTimeout = 20 * time.Second

	// DefaultPingInterval keeps under the 18 second interval KuCoin
	// advertises with the token.
	DefaultPingInterval = 10 * time.Second

	// DefaultReadTimeout outlasts one ping round trip on a quiet market. A
	// read deadline equal to the ping interval would race the pong.
	DefaultReadTimeout = 20 * time.Second

	// DefaultWriteInterval spaces subscriptions under KuCoin's limit of 100
	// messages per 10 seconds.
	DefaultWriteInterval = 300 * time.Millisecond

	// DefaultMaxTickersPerConnection keeps each connection well under
	// KuCoin's limit of 300 subscriptions.
	DefaultMaxTickersPerConnection = 25
)

// DefaultWebSocketConfig is the default configuration for the KuCoin
// websocket.
var DefaultWebSocketConfig = websocket.Config{
	Name:                     Name,
	MaxBufferSize:            websocket.DefaultMaxBufferSize,
	ReconnectionTimeout:      websocket.DefaultReconnectionTimeout,
	PostConnectionTimeout:    websocket.DefaultPostConnectionTimeout,
	HandshakeTimeout:         DefaultHandshakeTimeout,
	Endpoints:                []types.Endpoint{{URL: URL}},
	EnableCompression:        websocket.DefaultEnableCompression,
	ReadTimeout:              DefaultReadTimeout,
	WriteTimeout:             websocket.DefaultWriteTimeout,
	PingInterval:             DefaultPingInterval,
	WriteInterval:            DefaultWriteInterval,
	MaxTickersPerConnection:  DefaultMaxTickersPerConnection,
	MaxSubscriptionsPerBatch: websocket.DefaultMaxSubscriptionsPerBatch,
}

// DefaultMarkets defines the built-in KuCoin pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDC/USDT", Symbol: "USDC-USDT"},
}
