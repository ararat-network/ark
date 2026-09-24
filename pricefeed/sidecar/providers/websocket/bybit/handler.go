// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bybit/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bybit

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Bybit websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Bybit websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
}

// NewHandler returns a new Bybit DataHandler.
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

// HandleMessage handles subscribe responses, pong responses, and ticker
// updates. A ticker update carries no op, so it is whatever a request
// response is not.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseResponse
	)

	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch Operation(base.Op) {
	case OperationSubscribe:
		if !base.Success {
			return resp, nil, fmt.Errorf("subscription failed: %s", base.RetMsg)
		}

		h.logger.Debug("successfully subscribed", "connection", base.ConnID)
		return resp, nil, nil
	case OperationPing:
		h.logger.Debug("received pong")
		return resp, nil, nil
	case "":
		var update TickerUpdateMessage
		if err := json.Unmarshal(message, &update); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal ticker message: %w", err)
		}

		resp, err := h.parseTickerUpdate(update)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown op %q", base.Op)
	}
}

// CreateMessages subscribes to the tickers topic of each requested symbol.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	topics := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		topics = append(topics, string(TickerChannel)+TopicSeparator+string(ticker))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(topics)
}

// HeartBeatMessages returns the client ping Bybit expects.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return NewHeartbeatMessage()
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger: h.logger,
		config: h.config,
		cache:  types.NewTickers(),
	}
}
