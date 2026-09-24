// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/gate/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package gate

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Gate.io websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Gate.io websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
	// nextID is the next subscribe request id.
	nextID int64
}

// NewHandler returns a new Gate.io DataHandler.
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
		nextID: 1,
	}, nil
}

// HandleMessage handles pong answers, subscribe responses, and ticker
// updates.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseMessage
	)

	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	if Channel(base.Channel) == ChannelPong {
		h.logger.Debug("received pong")
		return resp, nil, nil
	}

	switch Event(base.Event) {
	case EventSubscribe:
		var msg SubscribeResponse
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal subscribe response: %w", err)
		}

		return resp, nil, h.parseSubscribeResponse(msg)
	case EventUpdate:
		var msg TickerUpdate
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal ticker update: %w", err)
		}

		resp, err := h.parseTickerUpdate(msg)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown event %q on channel %q", base.Event, base.Channel)
	}
}

// CreateMessages subscribes the requested pairs to the tickers channel.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	pairs := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		pairs = append(pairs, string(ticker))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(pairs)
}

// HeartBeatMessages returns the client ping, which keeps a quiet market's
// reads flowing.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return NewPingMessage()
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger: h.logger,
		config: h.config,
		cache:  types.NewTickers(),
		nextID: 1,
	}
}
