// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/cryptodotcom/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cryptodotcom

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type (
	// Method is the method of a Crypto.com message.
	Method string

	// StatusCode is the code of a Crypto.com response.
	StatusCode int64
)

const (
	// SubscribeMethod subscribes to channels and also labels the updates
	// those channels push.
	SubscribeMethod Method = "subscribe"
	// HeartbeatRequestMethod is the server heartbeat, sent every 30 seconds.
	HeartbeatRequestMethod Method = "public/heartbeat"
	// HeartbeatResponseMethod answers a server heartbeat with the same id.
	HeartbeatResponseMethod Method = "public/respond-heartbeat"

	// SuccessStatusCode is the code of a successful response.
	SuccessStatusCode StatusCode = 0

	// TickerChannelFormat is the ticker channel of one instrument.
	TickerChannelFormat = "ticker.%s"
)

// HeartbeatResponseMessage answers a server heartbeat. See README.md for
// wire examples.
type HeartbeatResponseMessage struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
}

// NewHeartbeatResponseMessage builds the answer to the server heartbeat id.
func NewHeartbeatResponseMessage(id int64) ([]byte, error) {
	return json.Marshal(HeartbeatResponseMessage{
		ID:     id,
		Method: string(HeartbeatResponseMethod),
	})
}

// SubscribeRequestMessage subscribes to channels.
type SubscribeRequestMessage struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params SubscribeParams `json:"params"`
}

// SubscribeParams names the channels to subscribe to.
type SubscribeParams struct {
	Channels []string `json:"channels"`
}

// NewSubscribeRequestMessages builds subscribe requests for channels, at
// most MaxSubscriptionsPerBatch channels per request.
func (h *Handler) NewSubscribeRequestMessages(channels []string) ([][]byte, error) {
	if len(channels) == 0 {
		return nil, errors.New("no channels to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(channels, h.config.MaxSubscriptionsPerBatch) {
		msg, err := json.Marshal(SubscribeRequestMessage{
			ID:     h.nextRequestID(),
			Method: string(SubscribeMethod),
			Params: SubscribeParams{Channels: batch},
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

// ResponseMessage is every message the server sends: heartbeats, subscribe
// acknowledgements, and ticker updates share the shape. An acknowledgement
// carries no result data.
type ResponseMessage struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Code   int64  `json:"code"`
	Result Result `json:"result"`
}

// Result carries the instrument data of a ticker update.
type Result struct {
	Data []InstrumentData `json:"data"`
}

// InstrumentData carries the fields this adapter reads from one instrument's
// ticker. LatestTradePrice is null when the instrument has not traded.
type InstrumentData struct {
	LatestTradePrice string `json:"a"`
	Name             string `json:"i"`
}
