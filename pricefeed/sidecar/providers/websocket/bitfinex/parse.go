// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bitfinex/parse.go.
// Modified for Ark: frame decoding and price parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitfinex

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// subscribed binds a confirmed channel id to its ticker. The confirmation
// reports the pair without the t prefix and the symbol with it, so both
// forms of the configured symbol match.
func (h *Handler) subscribed(msg SubscribedMessage) error {
	ticker, ok := h.cache.Lookup(msg.Pair)
	if !ok {
		ticker, ok = h.cache.Lookup(msg.Symbol)
	}
	if !ok {
		return fmt.Errorf("subscribed to unknown pair %s", msg.Pair)
	}

	h.channels[msg.ChannelID] = ticker
	return nil
}

// handleStream decodes a stream frame: a channel id, then either the
// heartbeat marker or a ticker payload. See README.md for the payload layout.
func (h *Handler) handleStream(message []byte) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	var frame []json.RawMessage
	if err := json.Unmarshal(message, &frame); err != nil {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("failed to unmarshal message: %w", err)
	}
	if len(frame) != FrameLength {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("invalid frame length %d, expected %d", len(frame), FrameLength)
	}

	var channelID int
	if err := json.Unmarshal(frame[0], &channelID); err != nil {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid channel id: %w", err)
	}
	ticker, ok := h.channels[channelID]
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("received stream for unknown channel id %d", channelID)
	}

	var marker string
	if err := json.Unmarshal(frame[1], &marker); err == nil {
		if marker == HeartbeatID {
			h.logger.Debug("received heartbeat", "ticker", ticker)
			return types.NewResponse(resolved, unresolved), nil
		}

		return types.NewResponse(resolved, unresolved), fmt.Errorf("unknown payload %q", marker)
	}

	var payload []json.RawMessage
	if err := json.Unmarshal(frame[1], &payload); err != nil || len(payload) < MinTickerPayloadLength {
		unresolved[ticker] = types.NewErrorWithCode(
			fmt.Errorf("invalid ticker payload for %s: need at least %d elements", ticker, MinTickerPayloadLength),
			types.ErrorInvalidResponse,
		)
		return types.NewResponse(resolved, unresolved), nil
	}

	var lastPrice json.Number
	if err := json.Unmarshal(payload[LastPriceIndex], &lastPrice); err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}
	price, err := sidecartypes.ParsePrice(lastPrice.String())
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}
