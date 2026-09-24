// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/mexc/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package mexc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for MEXC websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the MEXC websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
}

// NewHandler returns a new MEXC DataHandler.
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

// HandleMessage handles acknowledgements, which are JSON objects, and
// ticker pushes, which are protobuf frames. An acknowledgement echoes the
// stream it subscribed, answers a ping, or names a refusal; a refusal
// arrives with a zero code, so the message text is what tells it apart.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var resp types.Response

	if len(message) == 0 {
		return resp, nil, errors.New("empty message")
	}

	// Acknowledgements are JSON objects; a frame opens with its channel tag.
	if message[0] != '{' {
		push, err := DecodePush(message)
		if err != nil {
			return resp, nil, err
		}

		resp, err := h.parsePush(push)
		return resp, nil, err
	}

	var base BaseMessage
	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch {
	case base.Code != 0:
		return resp, nil, fmt.Errorf("mexc error %d: %s", base.Code, base.Message)
	case strings.HasPrefix(base.Message, string(MiniTickerChannel)):
		h.logger.Debug("successfully subscribed", "streams", base.Message)
		return resp, nil, nil
	case base.Message == PongMessage:
		h.logger.Debug("received pong")
		return resp, nil, nil
	default:
		return resp, nil, fmt.Errorf("mexc refused: %q", base.Message)
	}
}

// CreateMessages subscribes to the mini ticker stream of each requested
// symbol. The fetcher shards tickers under MaxTickersPerConnection; the
// check here refuses a shard that would exceed the venue's limit anyway.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	if h.config.MaxTickersPerConnection > 0 && len(tickers) > h.config.MaxTickersPerConnection {
		return nil, fmt.Errorf(
			"cannot subscribe to %d tickers on one connection; the limit is %d",
			len(tickers),
			h.config.MaxTickersPerConnection,
		)
	}

	streams := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		streams = append(streams, StreamName(string(ticker)))
		h.cache.Add(ticker)
	}

	return h.NewSubscribeRequestMessages(streams)
}

// HeartBeatMessages returns the client ping MEXC expects.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return NewPingMessage()
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger: h.logger,
		config: h.config,
		cache:  types.NewTickers(),
	}
}
