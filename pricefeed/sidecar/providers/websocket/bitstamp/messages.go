// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bitstamp/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitstamp

import (
	"encoding/json"
)

type (
	// ChannelType is a Bitstamp channel prefix.
	ChannelType string

	// EventType is the event of a Bitstamp message.
	EventType string
)

const (
	// TradeChannelPrefix prefixes the live trades channel, which pushes every
	// trade on the market that follows it.
	TradeChannelPrefix ChannelType = "live_trades_"

	// HeartbeatEvent is sent by the client to keep the connection alive and
	// echoed by the server.
	HeartbeatEvent EventType = "bts:heartbeat"
	// SubscribeEvent requests a channel subscription.
	SubscribeEvent EventType = "bts:subscribe"
	// SubscriptionSucceededEvent confirms a subscription.
	SubscriptionSucceededEvent EventType = "bts:subscription_succeeded"
	// RequestReconnectEvent warns that the server is about to close the
	// connection.
	RequestReconnectEvent EventType = "bts:request_reconnect"
	// ErrorEvent reports a failed request.
	ErrorEvent EventType = "bts:error"
	// TradeEvent carries one trade on a subscribed channel.
	TradeEvent EventType = "trade"
)

// BaseMessage identifies a message by its event.
type BaseMessage struct {
	Event string `json:"event"`
}

// SubscribeRequestMessage requests a channel subscription. See README.md for
// wire examples.
type SubscribeRequestMessage struct {
	Event string               `json:"event"`
	Data  SubscribeRequestData `json:"data"`
}

// SubscribeRequestData names the channel to subscribe to.
type SubscribeRequestData struct {
	Channel string `json:"channel"`
}

// NewSubscribeRequestMessage builds a subscription for channel.
func NewSubscribeRequestMessage(channel string) ([]byte, error) {
	return json.Marshal(SubscribeRequestMessage{
		Event: string(SubscribeEvent),
		Data:  SubscribeRequestData{Channel: channel},
	})
}

// SubscriptionResponseMessage confirms a subscription.
type SubscriptionResponseMessage struct {
	Event   string `json:"event"`
	Channel string `json:"channel"`
}

// ErrorMessage reports a failed request.
type ErrorMessage struct {
	Event string    `json:"event"`
	Data  ErrorData `json:"data"`
}

// ErrorData carries the error code and message.
type ErrorData struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

// TradeMessage carries one trade; PriceStr is the observation used by this
// adapter.
type TradeMessage struct {
	Event   string    `json:"event"`
	Channel string    `json:"channel"`
	Data    TradeData `json:"data"`
}

// TradeData carries the fields this adapter reads from a trade.
type TradeData struct {
	PriceStr string `json:"price_str"`
}

// NewHeartbeatRequestMessage builds the client heartbeat.
func NewHeartbeatRequestMessage() ([][]byte, error) {
	bz, err := json.Marshal(BaseMessage{Event: string(HeartbeatEvent)})
	if err != nil {
		return nil, err
	}

	return [][]byte{bz}, nil
}
