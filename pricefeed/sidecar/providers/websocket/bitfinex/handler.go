// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bitfinex/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitfinex

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Bitfinex websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Bitfinex websocket.
	config websocket.Config
	// channels maps a confirmed channel id to the ticker it streams.
	channels map[int]types.Ticker
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
}

// NewHandler returns a new Bitfinex DataHandler.
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
		logger:   logger.With("websocket_data_handler", config.Name),
		config:   config,
		channels: make(map[int]types.Ticker),
		cache:    types.NewTickers(),
	}, nil
}

// HandleMessage handles subscription confirmations, error and info notices,
// and stream frames. Confirmations bind a channel id to its ticker; frames
// carry heartbeats or ticker payloads for that channel id.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseMessage
	)

	// Stream frames are JSON arrays, which do not decode into an object.
	if err := json.Unmarshal(message, &base); err != nil {
		resp, err := h.handleStream(message)
		return resp, nil, err
	}

	switch Event(base.Event) {
	case EventSubscribed:
		var msg SubscribedMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal subscribed message: %w", err)
		}
		if err := h.subscribed(msg); err != nil {
			return resp, nil, err
		}

		h.logger.Debug("successfully subscribed", "pair", msg.Pair, "channel_id", msg.ChannelID)
		return resp, nil, nil
	case EventError:
		var msg ErrorMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal error message: %w", err)
		}

		return resp, nil, fmt.Errorf("bitfinex error %d (%s): %w", msg.Code, msg.Msg, ErrorCode(msg.Code).Error())
	case EventInfo:
		h.logger.Debug("received info message")
		return resp, nil, nil
	default:
		return resp, nil, fmt.Errorf("unknown event %q", base.Event)
	}
}

// CreateMessages subscribes to the ticker channel for each requested symbol,
// one message per symbol, since Bitfinex does not batch subscriptions.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	if len(tickers) == 0 {
		return nil, errors.New("no tickers to subscribe to")
	}

	msgs := make([][]byte, 0, len(tickers))
	for _, ticker := range tickers {
		msg, err := NewSubscribeMessage(string(ticker))
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}

		msgs = append(msgs, msg)
		h.cache.Add(ticker)
	}

	return msgs, nil
}

// HeartBeatMessages returns nil because Bitfinex sends its own heartbeat
// frames and needs none from the client.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger:   h.logger,
		config:   h.config,
		channels: make(map[int]types.Ticker),
		cache:    types.NewTickers(),
	}
}
