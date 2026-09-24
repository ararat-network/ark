// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/kraken/ws_data_handler.go.
// Modified for Ark: websocket v2 provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kraken

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Kraken websocket v2 messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Kraken websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
}

// NewHandler returns a new Kraken DataHandler.
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

// HandleMessage handles data pushes, told apart by channel, and request
// responses, told apart by method. A failed subscription is reported, not
// retried: the venue answers a bad symbol the same way every time.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseMessage
	)

	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch {
	case Channel(base.Channel) == ChannelStatus:
		var msg StatusMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal status message: %w", err)
		}

		return resp, nil, h.parseStatusMessage(msg)
	case Channel(base.Channel) == ChannelHeartbeat:
		h.logger.Debug("received heartbeat")
		return resp, nil, nil
	case Channel(base.Channel) == ChannelTicker:
		var msg TickerMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal ticker message: %w", err)
		}

		return h.parseTickerMessage(msg), nil, nil
	case Method(base.Method) == MethodSubscribe:
		var msg MethodResponse
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal subscribe response: %w", err)
		}

		return resp, nil, h.parseSubscribeResponse(msg)
	case Method(base.Method) == MethodPong:
		h.logger.Debug("received pong")
		return resp, nil, nil
	default:
		return resp, nil, fmt.Errorf("unknown message with channel %q and method %q", base.Channel, base.Method)
	}
}

// CreateMessages subscribes the requested symbols to the ticker channel.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	symbols := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		symbols = append(symbols, string(ticker))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(symbols)
}

// HeartBeatMessages returns nil because Kraken sends its own heartbeat and
// needs none from the client.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger: h.logger,
		config: h.config,
		cache:  types.NewTickers(),
	}
}
