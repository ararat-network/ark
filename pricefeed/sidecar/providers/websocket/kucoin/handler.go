// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/kucoin/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kucoin

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for KuCoin websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the KuCoin websocket.
	config websocket.Config
	// sequences holds the latest sequence number seen per ticker.
	sequences map[types.Ticker]int64
	// cache maps requested symbols to their configured tickers.
	cache types.Tickers
	// observed contains tickers with a valid price received in this connection.
	observed types.Tickers
	// nextID is the next subscribe request id.
	nextID int64
}

// NewHandler returns a new KuCoin DataHandler.
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
		cache:     types.NewTickers(),
		observed:  types.NewTickers(),
		nextID:    1,
	}, nil
}

// HandleMessage handles the welcome, pong answers, subscription acknowledgements,
// errors, and ticker data. Pongs refresh only tickers with a valid price observed
// in this connection.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseMessage
	)

	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch MessageType(base.Type) {
	case WelcomeMessage:
		h.logger.Debug("received welcome")
		return resp, nil, nil
	case PongMessage:
		h.logger.Debug("received pong")
		return types.NewUnchangedResponse(h.observed.All(), time.Now().UTC()), nil, nil
	case AckMessage:
		h.logger.Debug("subscription acknowledged", "id", base.ID)
		return resp, nil, nil
	case ErrorMessage:
		var msg ErrorResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal error message: %w", err)
		}

		return resp, nil, fmt.Errorf("kucoin error %d for request %s: %s", msg.Code, msg.ID, msg.Data)
	case DataMessage:
		var msg TickerResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal ticker message: %w", err)
		}

		resp, err := h.parseTickerMessage(msg)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown message type %q", base.Type)
	}
}

// CreateMessages subscribes to the ticker topic of the requested symbols.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	symbols := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		symbols = append(symbols, string(ticker))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(symbols)
}

// HeartBeatMessages returns the client ping KuCoin expects.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return NewHeartbeatMessage()
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger:    h.logger,
		config:    h.config,
		sequences: make(map[types.Ticker]int64),
		cache:     types.NewTickers(),
		observed:  types.NewTickers(),
		nextID:    1,
	}
}
