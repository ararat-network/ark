// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/gate/messages.go.
// Modified for Ark: websocket message types and validation.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

type (
	// ErrorCode is an error code reported by the Gate.io websocket.
	ErrorCode int64

	// Channel is a Gate.io channel.
	Channel string

	// Event is the event of a Gate.io message.
	Event string

	// Status is the status of a Gate.io request result.
	Status string
)

const (
	// ChannelTickers is the spot tickers channel.
	ChannelTickers Channel = "spot.tickers"
	// ChannelPing is the client ping.
	ChannelPing Channel = "spot.ping"
	// ChannelPong answers the client ping.
	ChannelPong Channel = "spot.pong"

	// EventSubscribe requests a subscription and labels its response.
	EventSubscribe Event = "subscribe"
	// EventUpdate labels a channel update.
	EventUpdate Event = "update"

	// StatusSuccess is the status of a successful request.
	StatusSuccess Status = "success"

	// ErrorInvalidRequestBody is returned for an invalid request body.
	ErrorInvalidRequestBody ErrorCode = 1
	// ErrorInvalidArgument is returned for an invalid argument.
	ErrorInvalidArgument ErrorCode = 2
	// ErrorServer is returned for a server-side failure.
	ErrorServer ErrorCode = 3
)

// Error returns the error for the code.
func (e ErrorCode) Error() error {
	switch e {
	case ErrorInvalidRequestBody:
		return errors.New("invalid body in request")
	case ErrorInvalidArgument:
		return errors.New("invalid argument in request")
	case ErrorServer:
		return errors.New("server side error")
	default:
		return errors.New("unknown error")
	}
}

// BaseMessage carries the fields every message has.
type BaseMessage struct {
	Time    int64  `json:"time"`
	Channel string `json:"channel"`
	Event   string `json:"event"`
}

// ErrorMessage is the error of a failed request.
type ErrorMessage struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

// RequestResult is the result of a request.
type RequestResult struct {
	Status string `json:"status"`
}

// SubscribeRequest subscribes currency pairs to a channel. See README.md for
// wire examples.
type SubscribeRequest struct {
	BaseMessage
	ID      int64    `json:"id"`
	Payload []string `json:"payload"`
}

// NewSubscribeRequestMessages builds tickers subscriptions for pairs, at
// most MaxSubscriptionsPerBatch pairs per request.
func (h *Handler) NewSubscribeRequestMessages(pairs []string) ([][]byte, error) {
	if len(pairs) == 0 {
		return nil, errors.New("no pairs to subscribe to")
	}

	var msgs [][]byte
	for batch := range slices.Chunk(pairs, h.config.MaxSubscriptionsPerBatch) {
		now := time.Now().Unix()
		msg, err := json.Marshal(SubscribeRequest{
			BaseMessage: BaseMessage{
				Time:    now,
				Channel: string(ChannelTickers),
				Event:   string(EventSubscribe),
			},
			ID:      h.nextRequestID(),
			Payload: batch,
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

// NewPingMessage builds the client ping.
func NewPingMessage() ([][]byte, error) {
	msg, err := json.Marshal(BaseMessage{
		Time:    time.Now().Unix(),
		Channel: string(ChannelPing),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ping message: %w", err)
	}

	return [][]byte{msg}, nil
}

// SubscribeResponse answers a subscribe request. Error is null on success.
type SubscribeResponse struct {
	BaseMessage
	ID     int64         `json:"id"`
	Error  *ErrorMessage `json:"error"`
	Result RequestResult `json:"result"`
}

// TickerUpdate carries one pair's ticker; Last is the observation used by
// this adapter.
type TickerUpdate struct {
	BaseMessage
	Result TickerResult `json:"result"`
}

// TickerResult carries the fields this adapter reads from a ticker.
type TickerResult struct {
	CurrencyPair string `json:"currency_pair"`
	Last         string `json:"last"`
}
