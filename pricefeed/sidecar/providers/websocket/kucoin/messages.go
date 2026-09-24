// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/kucoin/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kucoin

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type (
	// MessageType is the type of a KuCoin message.
	MessageType string

	// Topic is a KuCoin subscription topic prefix.
	Topic string

	// Subject is the subject of a KuCoin data message.
	Subject string
)

const (
	// WelcomeMessage is sent when the connection opens.
	WelcomeMessage MessageType = "welcome"
	// PingMessage is the client heartbeat.
	PingMessage MessageType = "ping"
	// PongMessage answers the client heartbeat.
	PongMessage MessageType = "pong"
	// SubscribeMessage requests a subscription.
	SubscribeMessage MessageType = "subscribe"
	// AckMessage acknowledges a subscription.
	AckMessage MessageType = "ack"
	// ErrorMessage reports a failed request.
	ErrorMessage MessageType = "error"
	// DataMessage carries a subscribed topic's data.
	DataMessage MessageType = "message"

	// TickerTopic prefixes the spot ticker topic; symbols follow it,
	// comma-separated.
	TickerTopic Topic = "/market/ticker:"
	// TickerSubject is the subject of a ticker data message.
	TickerSubject Subject = "trade.ticker"
)

// BaseMessage identifies a message by its type.
type BaseMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// NewHeartbeatMessage builds the client ping. See README.md for wire
// examples.
func NewHeartbeatMessage() ([][]byte, error) {
	msg, err := json.Marshal(BaseMessage{
		ID:   strconv.FormatInt(time.Now().UnixMilli(), 10),
		Type: string(PingMessage),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ping message: %w", err)
	}

	return [][]byte{msg}, nil
}

// SubscribeRequestMessage subscribes to a topic.
type SubscribeRequestMessage struct {
	ID             int64  `json:"id"`
	Type           string `json:"type"`
	Topic          string `json:"topic"`
	PrivateChannel bool   `json:"privateChannel"`
	Response       bool   `json:"response"`
}

// NewSubscribeRequestMessages builds ticker subscriptions for symbols, at
// most MaxSubscriptionsPerBatch symbols per request.
func (h *Handler) NewSubscribeRequestMessages(symbols []string) ([][]byte, error) {
	if len(symbols) == 0 {
		return nil, errors.New("no symbols to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(symbols, h.config.MaxSubscriptionsPerBatch) {
		msg, err := json.Marshal(SubscribeRequestMessage{
			ID:             h.nextRequestID(),
			Type:           string(SubscribeMessage),
			Topic:          string(TickerTopic) + strings.Join(batch, ","),
			PrivateChannel: false,
			Response:       true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

// nextRequestID returns the next sequential request id.
func (h *Handler) nextRequestID() int64 {
	id := h.nextID
	h.nextID++
	return id
}

// ErrorResponseMessage reports a failed request.
type ErrorResponseMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Code int64  `json:"code"`
	Data string `json:"data"`
}

// TickerResponseMessage carries one symbol's ticker; Price is the
// observation used by this adapter, and Sequence orders it.
type TickerResponseMessage struct {
	Type    string     `json:"type"`
	Topic   string     `json:"topic"`
	Subject string     `json:"subject"`
	Data    TickerData `json:"data"`
}

// TickerData carries the fields this adapter reads from a ticker.
type TickerData struct {
	Sequence string `json:"sequence"`
	Price    string `json:"price"`
}
