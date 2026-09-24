// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/kraken/messages.go.
// Modified for Ark: websocket v2 message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kraken

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type (
	// Channel is the channel of a Kraken data push.
	Channel string

	// Method is the method of a Kraken request or its response.
	Method string

	// Status is the platform status.
	Status string
)

const (
	// ChannelStatus carries the platform status, sent on connect.
	ChannelStatus Channel = "status"
	// ChannelHeartbeat is sent every second when no subscription traffic
	// flows.
	ChannelHeartbeat Channel = "heartbeat"
	// ChannelTicker carries ticker snapshots and updates.
	ChannelTicker Channel = "ticker"

	// MethodSubscribe requests a subscription and labels its response.
	MethodSubscribe Method = "subscribe"
	// MethodPong answers a client ping.
	MethodPong Method = "pong"

	// SystemOnline is the platform status that serves data.
	SystemOnline Status = "online"
)

// BaseMessage identifies a message: a data push carries a channel, a
// request response carries a method.
type BaseMessage struct {
	Channel string `json:"channel"`
	Method  string `json:"method"`
}

// SubscribeRequest subscribes symbols to a channel. See README.md for wire
// examples.
type SubscribeRequest struct {
	Method string          `json:"method"`
	Params SubscribeParams `json:"params"`
}

// SubscribeParams names the channel and symbols.
type SubscribeParams struct {
	Channel string   `json:"channel"`
	Symbol  []string `json:"symbol"`
}

// NewSubscribeRequestMessages builds ticker subscriptions for symbols, at
// most MaxSubscriptionsPerBatch symbols per request.
func (h *Handler) NewSubscribeRequestMessages(symbols []string) ([][]byte, error) {
	if len(symbols) == 0 {
		return nil, errors.New("no symbols to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(symbols, h.config.MaxSubscriptionsPerBatch) {
		msg, err := json.Marshal(SubscribeRequest{
			Method: string(MethodSubscribe),
			Params: SubscribeParams{Channel: string(ChannelTicker), Symbol: batch},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

// MethodResponse answers a request, one per symbol for a subscription.
// Error is set when Success is false.
type MethodResponse struct {
	Method  string          `json:"method"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Symbol  string          `json:"symbol"`
	Result  SubscribeResult `json:"result"`
}

// SubscribeResult names what a successful subscription holds.
type SubscribeResult struct {
	Channel string `json:"channel"`
	Symbol  string `json:"symbol"`
}

// StatusMessage carries the platform status.
type StatusMessage struct {
	Channel string       `json:"channel"`
	Data    []StatusData `json:"data"`
}

// StatusData carries the fields this adapter reads from a status entry.
type StatusData struct {
	System string `json:"system"`
}

// TickerMessage carries ticker snapshots or updates; Last is the observation
// used by this adapter and is kept as its decimal text.
type TickerMessage struct {
	Channel string       `json:"channel"`
	Type    string       `json:"type"`
	Data    []TickerData `json:"data"`
}

// TickerData carries the fields this adapter reads from a ticker entry.
type TickerData struct {
	Symbol string      `json:"symbol"`
	Last   json.Number `json:"last"`
}
