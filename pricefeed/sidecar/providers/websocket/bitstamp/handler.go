// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bitstamp/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitstamp

import (
	"encoding/json"
	"errors"
	"fmt"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Bitstamp websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Bitstamp websocket.
	config websocket.Config
	// cache maintains the latest set of tickers seen by the handler.
	cache types.Tickers
}

// NewHandler returns a new Bitstamp DataHandler.
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

// HandleMessage handles heartbeat echoes, subscription confirmations,
// reconnect warnings, errors, and trades. A reconnect warning is only
// logged: the server closes the connection shortly after, and the fetcher
// reconnects on the failed read.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var (
		resp types.Response
		base BaseMessage
	)

	if err := json.Unmarshal(message, &base); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch EventType(base.Event) {
	case HeartbeatEvent:
		h.logger.Debug("received heartbeat")
		return resp, nil, nil
	case RequestReconnectEvent:
		h.logger.Info("bitstamp requested a reconnect; the fetcher reconnects when the server closes")
		return resp, nil, nil
	case SubscriptionSucceededEvent:
		var msg SubscriptionResponseMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal subscription message: %w", err)
		}

		h.logger.Debug("successfully subscribed", "channel", msg.Channel)
		return resp, nil, nil
	case ErrorEvent:
		var msg ErrorMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal error message: %w", err)
		}

		return resp, nil, fmt.Errorf("bitstamp error %d: %s", msg.Data.Code, msg.Data.Message)
	case TradeEvent:
		var msg TradeMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal trade message: %w", err)
		}

		resp, err := h.parseTradeMessage(msg)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown event %q", base.Event)
	}
}

// CreateMessages subscribes to the live trades channel of each requested
// market, one message per market, since Bitstamp does not batch
// subscriptions.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	if len(tickers) == 0 {
		return nil, errors.New("no tickers to subscribe to")
	}

	msgs := make([][]byte, 0, len(tickers))
	for _, ticker := range tickers {
		msg, err := NewSubscribeRequestMessage(string(TradeChannelPrefix) + string(ticker))
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscribe message: %w", err)
		}

		msgs = append(msgs, msg)
		h.cache.Add(ticker)
	}

	return msgs, nil
}

// HeartBeatMessages returns the client heartbeat Bitstamp expects.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return NewHeartbeatRequestMessage()
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger: h.logger,
		config: h.config,
		cache:  types.NewTickers(),
	}
}
