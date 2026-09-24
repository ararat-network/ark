// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bybit/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bybit

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
)

type (
	// Operation is the op of a Bybit request or response.
	Operation string

	// Channel is a Bybit subscription channel.
	Channel string
)

const (
	// OperationSubscribe subscribes to topics.
	OperationSubscribe Operation = "subscribe"
	// OperationPing is the client heartbeat; the server answers with the
	// same op.
	OperationPing Operation = "ping"

	// TickerChannel is the spot tickers channel.
	TickerChannel Channel = "tickers"

	// TopicSeparator separates the channel from the symbol in a topic.
	TopicSeparator = "."
)

// BaseRequest carries the fields every request has.
type BaseRequest struct {
	ReqID string `json:"req_id,omitempty"`
	Op    string `json:"op"`
}

// SubscribeRequest subscribes to topics. See README.md for wire examples.
type SubscribeRequest struct {
	BaseRequest
	Args []string `json:"args"`
}

// NewSubscribeRequestMessages builds subscribe requests for topics, at most
// MaxSubscriptionsPerBatch topics per request.
func (h *Handler) NewSubscribeRequestMessages(topics []string) ([][]byte, error) {
	if len(topics) == 0 {
		return nil, errors.New("no topics to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(topics, h.config.MaxSubscriptionsPerBatch) {
		msg, err := json.Marshal(SubscribeRequest{
			BaseRequest: BaseRequest{Op: string(OperationSubscribe)},
			Args:        batch,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

// NewHeartbeatMessage builds the client ping.
func NewHeartbeatMessage() ([][]byte, error) {
	msg, err := json.Marshal(BaseRequest{
		ReqID: strconv.FormatInt(time.Now().UnixMilli(), 10),
		Op:    string(OperationPing),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal heartbeat message: %w", err)
	}

	return [][]byte{msg}, nil
}

// BaseResponse carries the fields every request response has. A ticker
// update has none of them.
type BaseResponse struct {
	Success bool   `json:"success"`
	RetMsg  string `json:"ret_msg"`
	ConnID  string `json:"conn_id"`
	Op      string `json:"op"`
}

// TickerUpdateMessage carries one symbol's ticker; LastPrice is the
// observation used by this adapter.
type TickerUpdateMessage struct {
	Topic string           `json:"topic"`
	Data  TickerUpdateData `json:"data"`
}

// TickerUpdateData carries the fields this adapter reads from a ticker.
type TickerUpdateData struct {
	Symbol    string `json:"symbol"`
	LastPrice string `json:"lastPrice"`
}
