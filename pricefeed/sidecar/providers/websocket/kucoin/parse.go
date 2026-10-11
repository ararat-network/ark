// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/kucoin/parse.go.
// Modified for Ark: ticker message parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kucoin

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// sequenceRestartRun is how many refused sequences in a row for one ticker
// read as KuCoin restarting the sequence rather than as late messages.
const sequenceRestartRun = 3

// parseTickerMessage converts a ticker data message into a provider
// response. A sequence at or below the last seen is refused so a late or
// repeated message cannot roll a price back, unless sequenceRestartRun of
// them arrive in a row, which takes the new sequence. A refused report stops
// pongs vouching for the ticker until a price is accepted again.
func (h *Handler) parseTickerMessage(msg TickerResponseMessage) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	if Subject(msg.Subject) != TickerSubject {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid subject %q", msg.Subject)
	}

	symbol, ok := strings.CutPrefix(msg.Topic, string(TickerTopic))
	if !ok || symbol == "" {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid ticker topic %q", msg.Topic)
	}

	ticker, ok := h.cache.Lookup(symbol)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", symbol)
	}

	sequence, err := strconv.ParseInt(msg.Data.Sequence, 10, 64)
	if err != nil {
		h.observed.Remove(ticker)
		unresolved[ticker] = types.NewErrorWithCode(
			fmt.Errorf("invalid sequence %q: %w", msg.Data.Sequence, err),
			types.ErrorInvalidResponse,
		)
		return types.NewResponse(resolved, unresolved), nil
	}
	if seen, ok := h.sequences[ticker]; ok && sequence <= seen {
		h.observed.Remove(ticker)
		h.backward[ticker]++
		if h.backward[ticker] < sequenceRestartRun {
			unresolved[ticker] = types.NewErrorWithCode(
				fmt.Errorf("received out of order message; sequence %d is not after %d", sequence, seen),
				types.ErrorInvalidResponse,
			)
			return types.NewResponse(resolved, unresolved), nil
		}
		h.logger.Warn("ticker sequence restarted; taking the new sequence", "ticker", ticker, "sequence", sequence, "previous", seen)
	}
	delete(h.backward, ticker)
	h.sequences[ticker] = sequence

	price, err := sidecartypes.ParsePrice(msg.Data.Price)
	if err != nil {
		h.observed.Remove(ticker)
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	h.observed.Add(ticker)
	return types.NewResponse(resolved, unresolved), nil
}
