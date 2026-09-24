// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/mexc/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package mexc

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type (
	// Method is the method of a MEXC request.
	Method string

	// Channel is a MEXC stream channel prefix.
	Channel string
)

const (
	// SubscriptionMethod subscribes to streams.
	SubscriptionMethod Method = "SUBSCRIPTION"
	// PingMethod is the client heartbeat.
	PingMethod Method = "PING"
	// PongMessage answers the client heartbeat.
	PongMessage = "PONG"

	// MiniTickerChannel prefixes the protobuf mini ticker stream; the
	// upper-case symbol and a time zone suffix follow it.
	MiniTickerChannel Channel = "spot@public.miniTicker.v3.api.pb@"
	// MiniTickerSuffix is the time zone suffix of a mini ticker stream.
	MiniTickerSuffix = "@UTC+8"
)

// BaseMessage is every acknowledgement MEXC sends: subscription echoes,
// refusals, and pongs share the shape. A refusal arrives with a zero code
// and a message that names no stream.
type BaseMessage struct {
	ID      int64  `json:"id"`
	Code    int64  `json:"code"`
	Message string `json:"msg"`
}

// RequestMessage is a request to MEXC. See README.md for wire examples.
type RequestMessage struct {
	Method string   `json:"method"`
	Params []string `json:"params,omitempty"`
}

// StreamName returns the mini ticker stream of symbol.
func StreamName(symbol string) string {
	return string(MiniTickerChannel) + strings.ToUpper(symbol) + MiniTickerSuffix
}

// NewSubscribeRequestMessages builds subscription requests for streams, at
// most MaxSubscriptionsPerBatch streams per request.
func (h *Handler) NewSubscribeRequestMessages(streams []string) ([][]byte, error) {
	if len(streams) == 0 {
		return nil, errors.New("no streams to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(streams, h.config.MaxSubscriptionsPerBatch) {
		msg, err := json.Marshal(RequestMessage{
			Method: string(SubscriptionMethod),
			Params: batch,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

// NewPingMessage builds the client ping.
func NewPingMessage() ([][]byte, error) {
	msg, err := json.Marshal(RequestMessage{Method: string(PingMethod)})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ping message: %w", err)
	}

	return [][]byte{msg}, nil
}
