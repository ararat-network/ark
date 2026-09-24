// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/huobi/ws_data_handler.go.
// Modified for Ark: websocket provider integration and message handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package huobi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

var _ websocket.DataHandler = (*Handler)(nil)

// Handler implements websocket.DataHandler for Huobi websocket messages.
type Handler struct {
	logger log.Logger

	// config is the config for the Huobi websocket.
	config websocket.Config
	// cache maps requested symbols to their configured tickers.
	cache types.Tickers
	// observed contains tickers with a valid price received in this connection.
	observed types.Tickers
}

// NewHandler returns a new Huobi DataHandler.
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
		cache:    types.NewTickers(),
		observed: types.NewTickers(),
	}, nil
}

// HandleMessage decompresses and handles server pings, subscription responses,
// and ticker streams. Pings are answered and refresh only tickers with a valid
// price observed in this connection. Every Huobi message is gzip-compressed,
// and each kind is told apart by the field only it carries.
func (h *Handler) HandleMessage(message []byte) (types.Response, [][]byte, error) {
	var resp types.Response

	payload, err := decompress(message)
	if err != nil {
		return resp, nil, err
	}

	var probe struct {
		Ping    int64  `json:"ping"`
		ID      string `json:"id"`
		Channel string `json:"ch"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return resp, nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	switch {
	case probe.Ping != 0:
		h.logger.Debug("received ping", "ping", probe.Ping)

		pong, err := NewPongMessage(PingMessage{Ping: probe.Ping})
		if err != nil {
			return resp, nil, err
		}
		return types.NewUnchangedResponse(h.observed.All(), time.Now().UTC()), pong, nil
	case probe.ID != "":
		var subscription SubscriptionResponse
		if err := json.Unmarshal(payload, &subscription); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal subscription response: %w", err)
		}

		return resp, nil, h.parseSubscriptionResponse(subscription)
	case probe.Channel != "":
		var stream TickerStream
		if err := json.Unmarshal(payload, &stream); err != nil {
			return resp, nil, fmt.Errorf("failed to unmarshal ticker stream: %w", err)
		}

		resp, err := h.parseTickerStream(stream)
		return resp, nil, err
	default:
		return resp, nil, fmt.Errorf("unknown message %s", payload)
	}
}

// CreateMessages subscribes to the ticker topic of each requested symbol,
// one message per symbol, since Huobi does not batch subscriptions.
func (h *Handler) CreateMessages(tickers []types.Ticker) ([][]byte, error) {
	if len(tickers) == 0 {
		return nil, errors.New("no tickers to subscribe to")
	}

	msgs := make([][]byte, 0, len(tickers))
	for _, ticker := range tickers {
		msg, err := NewSubscriptionRequest(string(ticker))
		if err != nil {
			return nil, fmt.Errorf("failed to marshal subscription message: %w", err)
		}

		msgs = append(msgs, msg)
		h.cache.Add(ticker)
	}

	return msgs, nil
}

// HeartBeatMessages returns nil because the server drives the heartbeat and
// the read loop answers it.
func (h *Handler) HeartBeatMessages() ([][]byte, error) {
	return nil, nil
}

// Copy returns an independent handler for one websocket connection.
func (h *Handler) Copy() websocket.DataHandler {
	return &Handler{
		logger:   h.logger,
		config:   h.config,
		cache:    types.NewTickers(),
		observed: types.NewTickers(),
	}
}

// decompress inflates a gzip message under MaxDecompressedBytes.
func decompress(message []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(message))
	if err != nil {
		return nil, fmt.Errorf("failed to open gzip message: %w", err)
	}
	defer reader.Close()

	payload, err := io.ReadAll(io.LimitReader(reader, MaxDecompressedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to decompress message: %w", err)
	}
	if len(payload) > MaxDecompressedBytes {
		return nil, fmt.Errorf("decompressed message exceeds %d bytes", MaxDecompressedBytes)
	}

	return payload, nil
}
