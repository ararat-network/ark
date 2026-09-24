// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/coinbase/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coinbase

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Coinbase websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Coinbase websocket.
	config websocket.Config
	// sequences holds the latest sequence number seen per ticker.
	sequences map[types.Ticker]int64
	// tradeIDs holds the latest trade id seen per ticker.
	tradeIDs map[types.Ticker]int64
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
}

// NewHandler returns a new Coinbase DataHandler.
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
		logger:    logger.With("websocket_data_handler", config.Name),
		config:    config,
		sequences: make(map[types.Ticker]int64),
		tradeIDs:  make(map[types.Ticker]int64),
		cache:     types.NewTickers(),
	}, nil
}

// HandleMessage handles subscription listings, errors, ticker matches, and
// heartbeats. A heartbeat whose last trade id matches the last match seen
// confirms the price still holds.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseMessage
	)

	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch MessageType(base.Type) {
	case SubscriptionsMessage:
		var msg SubscribeResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal subscriptions message: %w", err)
		}

		for _, channel := range msg.Channels {
			h.logger.Debug("subscribed", "channel", channel.Name, "products", channel.ProductIDs)
		}
		return resp, nil, nil
	case ErrorMessage:
		var msg ErrorResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal error message: %w", err)
		}

		return resp, nil, fmt.Errorf("coinbase error: %s: %s", msg.Message, msg.Reason)
	case TickerMessage:
		var msg TickerResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal ticker message: %w", err)
		}

		resp, err := h.parseTickerMessage(msg)
		return resp, nil, err
	case HeartbeatMessage:
		var msg HeartbeatResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal heartbeat message: %w", err)
		}

		resp, err := h.parseHeartbeatMessage(msg)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown message type %q", base.Type)
	}
}

// CreateMessages subscribes the requested products to the ticker and
// heartbeat channels.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	products := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		products = append(products, string(ticker))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(products)
}

// HeartBeatMessages returns nil because the heartbeat channel keeps the
// connection busy without a client message.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger:    h.logger,
		config:    h.config,
		sequences: make(map[types.Ticker]int64),
		tradeIDs:  make(map[types.Ticker]int64),
		cache:     types.NewTickers(),
	}
}
