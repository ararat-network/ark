// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bitfinex/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitfinex

import (
	"encoding/json"
	"errors"
)

type (
	// Event is the event of a Bitfinex JSON object message.
	Event string

	// Channel is a Bitfinex subscription channel.
	Channel string

	// ErrorCode is an error code reported by the Bitfinex websocket.
	ErrorCode int64
)

const (
	// EventSubscribe requests a subscription.
	EventSubscribe Event = "subscribe"
	// EventSubscribed confirms a subscription and carries its channel id.
	EventSubscribed Event = "subscribed"
	// EventError reports a failed request.
	EventError Event = "error"
	// EventInfo carries platform notices, including the version greeting
	// sent on connect.
	EventInfo Event = "info"

	// ChannelTicker is the ticker channel.
	ChannelTicker Channel = "ticker"

	// HeartbeatID is the payload of a heartbeat frame.
	HeartbeatID = "hb"
	// FrameLength is the length of every stream frame: channel id, payload.
	FrameLength = 2
	// LastPriceIndex is the index of the last price in a ticker payload:
	// bid, bid size, ask, ask size, daily change, daily change relative,
	// last price, volume, high, low, then whatever Bitfinex appends. The
	// documented ten fields are a floor, not the observed length.
	LastPriceIndex = 6
	// MinTickerPayloadLength is the shortest payload that carries the last
	// price.
	MinTickerPayloadLength = LastPriceIndex + 1

	// ErrorUnknownEvent is returned for an unknown event.
	ErrorUnknownEvent ErrorCode = 10000
	// ErrorUnknownPair is returned for an unknown pair.
	ErrorUnknownPair ErrorCode = 10001
	// ErrorLimitOpenChannels is returned when the connection holds too many
	// channels.
	ErrorLimitOpenChannels ErrorCode = 10305
	// ErrorSubscriptionFailed is returned when a subscription fails.
	ErrorSubscriptionFailed ErrorCode = 10400
	// ErrorNotSubscribed is returned for an unsubscribe of a channel that
	// was never subscribed.
	ErrorNotSubscribed ErrorCode = 10401
)

// Error returns the error for the code.
func (e ErrorCode) Error() error {
	switch e {
	case ErrorUnknownEvent:
		return errors.New("unknown event")
	case ErrorUnknownPair:
		return errors.New("unknown pair")
	case ErrorLimitOpenChannels:
		return errors.New("limit of open channels reached")
	case ErrorSubscriptionFailed:
		return errors.New("subscription failed")
	case ErrorNotSubscribed:
		return errors.New("not subscribed")
	default:
		return errors.New("unknown error")
	}
}

// BaseMessage identifies a JSON object message by its event. Stream frames
// are JSON arrays and do not decode into it.
type BaseMessage struct {
	Event string `json:"event"`
}

// SubscribeMessage requests a ticker subscription for one symbol. See
// README.md for wire examples.
type SubscribeMessage struct {
	Event   string `json:"event"`
	Channel string `json:"channel"`
	Symbol  string `json:"symbol"`
}

// NewSubscribeMessage builds a ticker subscription for symbol.
func NewSubscribeMessage(symbol string) ([]byte, error) {
	return json.Marshal(SubscribeMessage{
		Event:   string(EventSubscribe),
		Channel: string(ChannelTicker),
		Symbol:  symbol,
	})
}

// SubscribedMessage confirms a subscription. Pair is the symbol without its
// t prefix.
type SubscribedMessage struct {
	Event     string `json:"event"`
	Channel   string `json:"channel"`
	ChannelID int    `json:"chanId"`
	Symbol    string `json:"symbol"`
	Pair      string `json:"pair"`
}

// ErrorMessage reports a failed request.
type ErrorMessage struct {
	Event string `json:"event"`
	Msg   string `json:"msg"`
	Code  int64  `json:"code"`
}
