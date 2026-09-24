// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/coinbase/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coinbase

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type (
	// MessageType is the type of a Coinbase feed message.
	MessageType string

	// ChannelType is a Coinbase feed channel.
	ChannelType string
)

const (
	// SubscribeMessage requests subscriptions. It must arrive within five
	// seconds of connecting or the feed closes the connection.
	SubscribeMessage MessageType = "subscribe"
	// SubscriptionsMessage lists the channels the connection holds.
	SubscriptionsMessage MessageType = "subscriptions"
	// TickerMessage carries a match on a product.
	TickerMessage MessageType = "ticker"
	// HeartbeatMessage arrives every second per product with the last trade
	// id.
	HeartbeatMessage MessageType = "heartbeat"
	// ErrorMessage reports a failed request.
	ErrorMessage MessageType = "error"

	// TickerChannel pushes a message on every match.
	TickerChannel ChannelType = "ticker"
	// HeartbeatChannel pushes sequence and last trade id every second, which
	// lets a quiet market confirm its price still holds.
	HeartbeatChannel ChannelType = "heartbeat"
)

// BaseMessage identifies a message by its type.
type BaseMessage struct {
	Type string `json:"type"`
}

// SubscribeRequestMessage subscribes products to channels. See README.md for
// wire examples.
type SubscribeRequestMessage struct {
	Type       string   `json:"type"`
	ProductIDs []string `json:"product_ids"`
	Channels   []string `json:"channels"`
}

// NewSubscribeRequestMessages builds subscribe requests for products on the
// ticker and heartbeat channels, at most MaxSubscriptionsPerBatch products
// per request.
func (h *Handler) NewSubscribeRequestMessages(products []string) ([][]byte, error) {
	if len(products) == 0 {
		return nil, errors.New("no products to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(products, h.config.MaxSubscriptionsPerBatch) {
		msg, err := json.Marshal(SubscribeRequestMessage{
			Type:       string(SubscribeMessage),
			ProductIDs: batch,
			Channels:   []string{string(TickerChannel), string(HeartbeatChannel)},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

// SubscribeResponseMessage lists the channels the connection holds.
type SubscribeResponseMessage struct {
	Type     string    `json:"type"`
	Channels []Channel `json:"channels"`
}

// Channel is one held channel and its products.
type Channel struct {
	Name       string   `json:"name"`
	ProductIDs []string `json:"product_ids"`
}

// ErrorResponseMessage reports a failed request.
type ErrorResponseMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// TickerResponseMessage carries a match; Price is the observation used by
// this adapter, and Sequence and TradeID order it against heartbeats.
type TickerResponseMessage struct {
	Type      string `json:"type"`
	Sequence  int64  `json:"sequence"`
	ProductID string `json:"product_id"`
	Price     string `json:"price"`
	TradeID   int64  `json:"trade_id"`
}

// HeartbeatResponseMessage confirms a product's last trade id once a second.
type HeartbeatResponseMessage struct {
	Type        string `json:"type"`
	Sequence    int64  `json:"sequence"`
	LastTradeID int64  `json:"last_trade_id"`
	ProductID   string `json:"product_id"`
}
