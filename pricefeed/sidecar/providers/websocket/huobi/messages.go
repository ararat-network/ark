// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/huobi/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package huobi

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Status is the status of a subscription response.
type Status string

const (
	// StatusOK is the status of a successful subscription.
	StatusOK Status = "ok"

	// tickerTopicFormat is the ticker topic of one symbol.
	tickerTopicFormat = "market.%s.ticker"
	// topicParts is the number of dot-separated parts in a ticker topic.
	topicParts = 3
	// topicSymbolIndex is the index of the symbol in a ticker topic.
	topicSymbolIndex = 1
)

// TickerTopic returns the ticker topic for symbol.
func TickerTopic(symbol string) string {
	return fmt.Sprintf(tickerTopicFormat, symbol)
}

// SymbolFromTopic returns the symbol of a ticker topic, or an empty string
// when the topic is not one.
func SymbolFromTopic(topic string) string {
	parts := strings.Split(topic, ".")
	if len(parts) != topicParts {
		return ""
	}
	return parts[topicSymbolIndex]
}

// PingMessage is the server heartbeat. See README.md for wire examples.
type PingMessage struct {
	Ping int64 `json:"ping"`
}

// PongMessage answers a server heartbeat with the same value.
type PongMessage struct {
	Pong int64 `json:"pong"`
}

// NewPongMessage builds the answer to ping.
func NewPongMessage(ping PingMessage) ([][]byte, error) {
	msg, err := json.Marshal(PongMessage{Pong: ping.Ping})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pong message: %w", err)
	}

	return [][]byte{msg}, nil
}

// SubscriptionRequest subscribes to one topic; ID is echoed in the response.
type SubscriptionRequest struct {
	Sub string `json:"sub"`
	ID  string `json:"id"`
}

// NewSubscriptionRequest builds a ticker subscription for symbol.
func NewSubscriptionRequest(symbol string) ([]byte, error) {
	return json.Marshal(SubscriptionRequest{
		Sub: TickerTopic(symbol),
		ID:  symbol,
	})
}

// SubscriptionResponse answers a subscription request.
type SubscriptionResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Subbed string `json:"subbed"`
	ErrMsg string `json:"err-msg"`
}

// TickerStream carries one symbol's ticker; LastPrice is the observation
// used by this adapter and is kept as its decimal text.
type TickerStream struct {
	Channel string `json:"ch"`
	Tick    Tick   `json:"tick"`
}

// Tick carries the fields this adapter reads from a ticker.
type Tick struct {
	LastPrice json.Number `json:"lastPrice"`
}
