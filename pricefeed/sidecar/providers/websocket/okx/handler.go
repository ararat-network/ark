// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/okx/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package okx

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for OKX websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the OKX websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
}

// NewHandler returns a new OKX DataHandler.
func NewHandler(logger log.Logger, config websocket.Config) (websocket.DataHandler, error) {
	if logger == nil {
		return nil, errors.New("logger is nil")
	}
	if config.Name != Name {
		return nil, fmt.Errorf("expected websocket config name %s, got %s", Name, config.Name)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid websocket config for %s: %w", Name, err)
	}

	return &Handler{
		logger: logger.With("websocket_data_handler", config.Name),
		config: config,
		cache:  types.NewTickers(),
	}, nil
}

// HandleMessage handles the plain-text pong, subscription confirmations,
// connection counts, errors, and ticker pushes. A failed subscription is
// reported, not retried: the venue answers a bad instrument the same way
// every time.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseMessage
	)

	if string(message) == PongMessage {
		h.logger.Debug("received pong")
		return resp, nil, nil
	}

	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch EventType(base.Event) {
	case EventSubscribe, EventError:
		var msg SubscribeResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal subscribe response: %w", err)
		}
		if EventType(msg.Event) == EventError {
			return resp, nil, fmt.Errorf("okx error %s: %s", msg.Code, msg.Message)
		}

		h.logger.Debug("successfully subscribed", "instrument", msg.Arguments.InstrumentID)
		return resp, nil, nil
	case EventChannelConnCount, EventChannelConnCountError:
		var msg ChannelConnCountMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal connection count message: %w", err)
		}
		if EventType(msg.Event) == EventChannelConnCountError {
			return resp, nil, fmt.Errorf("okx channel %s connection limit exceeded at %s connections", msg.Channel, msg.ConnCount)
		}

		h.logger.Debug("channel connection count", "channel", msg.Channel, "count", msg.ConnCount)
		return resp, nil, nil
	case EventData:
		var msg TickersResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal tickers message: %w", err)
		}

		resp, err := h.parseTickersMessage(msg)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown event %q", base.Event)
	}
}

// CreateMessages subscribes each requested instrument to the tickers channel.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	topics := make([]SubscriptionTopic, 0, len(tickers))
	for _, ticker := range tickers {
		topics = append(topics, SubscriptionTopic{
			Channel:      string(TickersChannel),
			InstrumentID: string(ticker),
		})
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(topics)
}

// HeartBeatMessages returns the plain-text ping OKX expects.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return [][]byte{[]byte(PingMessage)}, nil
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger: h.logger,
		config: h.config,
		cache:  types.NewTickers(),
	}
}
