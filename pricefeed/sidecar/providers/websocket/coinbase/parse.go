// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/coinbase/parse.go.
// Modified for Ark: ticker and heartbeat parsing with unchanged results.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coinbase

import (
	"errors"
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseTickerMessage converts a match into a provider response and records
// its trade id for the heartbeats that follow.
func (h *Handler) parseTickerMessage(msg TickerResponseMessage) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	ticker, ok := h.cache.Lookup(msg.ProductID)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", msg.ProductID)
	}

	if err := h.checkSequence(ticker, msg.Sequence); err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorInvalidResponse)
		return types.NewResponse(resolved, unresolved), nil
	}

	price, err := sidecartypes.ParsePrice(msg.Price)
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	h.tradeIDs[ticker] = msg.TradeID
	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}

// parseHeartbeatMessage confirms a price that still holds. The heartbeat's
// last trade id must match the last match seen; a different id means a
// match was missed, so there is no price to confirm.
func (h *Handler) parseHeartbeatMessage(msg HeartbeatResponseMessage) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	ticker, ok := h.cache.Lookup(msg.ProductID)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got heartbeat for an unsupported market %s", msg.ProductID)
	}

	if err := h.checkSequence(ticker, msg.Sequence); err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorInvalidResponse)
		return types.NewResponse(resolved, unresolved), nil
	}

	tradeID, ok := h.tradeIDs[ticker]
	if !ok || tradeID != msg.LastTradeID {
		unresolved[ticker] = types.NewErrorWithCode(
			errors.New("no price update received for the heartbeat's last trade"),
			types.ErrorNoExistingPrice,
		)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewUnchangedResult(time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}

// checkSequence records sequence for ticker, refusing one older than the
// last seen so a late message cannot roll a price back.
func (h *Handler) checkSequence(ticker types.Ticker, sequence int64) error {
	if observed, ok := h.sequences[ticker]; ok && sequence < observed {
		return fmt.Errorf("received out of order message; sequence %d is older than %d", sequence, observed)
	}

	h.sequences[ticker] = sequence
	return nil
}
