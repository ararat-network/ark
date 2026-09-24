// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/cryptodotcom/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cryptodotcom

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Crypto.com websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Crypto.com websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
	// nextID is the next subscribe request id.
	nextID int64
}

// NewHandler returns a new Crypto.com DataHandler.
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

// HandleMessage handles server heartbeats, which must be answered with the
// same id or the server closes the connection, and subscribe messages,
// which are acknowledgements when they carry no data and ticker updates
// when they do.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		msg  ResponseMessage
	)

	if err := json.Unmarshal(message, &msg); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	if StatusCode(msg.Code) != SuccessStatusCode {
		return resp, nil, fmt.Errorf("crypto.com error code %d for %s request %d", msg.Code, msg.Method, msg.ID)
	}

	switch Method(msg.Method) {
	case HeartbeatRequestMethod:
		h.logger.Debug("received heartbeat", "id", msg.ID)

		heartbeat, err := NewHeartbeatResponseMessage(msg.ID)
		if err != nil {
			return resp, nil, fmt.Errorf("failed to marshal heartbeat response: %w", err)
		}
		return resp, [][]byte{heartbeat}, nil
	case SubscribeMethod:
		if len(msg.Result.Data) == 0 {
			h.logger.Debug("subscription acknowledged", "id", msg.ID)
			return resp, nil, nil
		}

		return h.parseInstrumentMessage(msg), nil, nil
	default:
		return resp, nil, fmt.Errorf("unknown method %q", msg.Method)
	}
}

// CreateMessages subscribes to the ticker channel of each requested
// instrument.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	channels := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		channels = append(channels, fmt.Sprintf(TickerChannelFormat, ticker))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(channels)
}

// HeartBeatMessages returns nil because the server drives the heartbeat and
// the read loop answers it.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
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
