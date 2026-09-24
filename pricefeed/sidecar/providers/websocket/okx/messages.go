// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/okx/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package okx

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type (
	// Operation is the op of an OKX request.
	Operation string

	// Channel is an OKX subscription channel.
	Channel string

	// EventType is the event of an OKX response. A data push carries none.
	EventType string
)

const (
	// OperationSubscribe subscribes to channels.
	OperationSubscribe Operation = "subscribe"

	// TickersChannel is the tickers channel, which pushes a spot
	// instrument's last price.
	TickersChannel Channel = "tickers"

	// EventSubscribe confirms a subscription.
	EventSubscribe EventType = "subscribe"
	// EventError reports a failed request.
	EventError EventType = "error"
	// EventChannelConnCount reports a channel's connection count after each
	// subscription.
	EventChannelConnCount EventType = "channel-conn-count"
	// EventChannelConnCountError reports that a channel's connection limit is
	// exceeded.
	EventChannelConnCountError EventType = "channel-conn-count-error"
	// EventData is the empty event of a data push.
	EventData EventType = ""

	// PingMessage is the client heartbeat, sent as plain text.
	PingMessage = "ping"
	// PongMessage answers the client heartbeat, as plain text.
	PongMessage = "pong"
)

// BaseMessage identifies a JSON message by its event.
type BaseMessage struct {
	Event string `json:"event"`
}

// SubscribeRequestMessage subscribes to channels. See README.md for wire
// examples.
type SubscribeRequestMessage struct {
	Operation string              `json:"op"`
	Arguments []SubscriptionTopic `json:"args"`
}

// SubscriptionTopic names a channel and instrument.
type SubscriptionTopic struct {
	Channel      string `json:"channel"`
	InstrumentID string `json:"instId"`
}

// NewSubscribeRequestMessages builds subscribe requests for topics, at most
// MaxSubscriptionsPerBatch topics per request.
func (h *Handler) NewSubscribeRequestMessages(topics []SubscriptionTopic) ([][]byte, error) {
	if len(topics) == 0 {
		return nil, errors.New("no topics to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(topics, h.config.MaxSubscriptionsPerBatch) {
		msg, err := json.Marshal(SubscribeRequestMessage{
			Operation: string(OperationSubscribe),
			Arguments: batch,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

// SubscribeResponseMessage confirms a subscription or reports a failure.
type SubscribeResponseMessage struct {
	Arguments    SubscriptionTopic `json:"arg"`
	Event        string            `json:"event"`
	ConnectionID string            `json:"connId"`
	Code         string            `json:"code,omitempty"`
	Message      string            `json:"msg,omitempty"`
}

// ChannelConnCountMessage reports a channel's connection count.
type ChannelConnCountMessage struct {
	Event        string `json:"event"`
	Channel      string `json:"channel"`
	ConnCount    string `json:"connCount"`
	ConnectionID string `json:"connId"`
}

// TickersResponseMessage carries ticker pushes; Last is the observation used
// by this adapter.
type TickersResponseMessage struct {
	Arguments SubscriptionTopic `json:"arg"`
	Data      []Ticker          `json:"data"`
}

// Ticker carries the fields this adapter reads from a ticker push.
type Ticker struct {
	InstrumentID string `json:"instId"`
	Last         string `json:"last"`
}
