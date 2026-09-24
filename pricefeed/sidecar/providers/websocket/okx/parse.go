// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/okx/parse.go.
// Modified for Ark: ticker push parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package okx

import (
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseTickersMessage converts a tickers push into a provider response.
// Instruments this connection did not subscribe are skipped.
func (h *Handler) parseTickersMessage(msg TickersResponseMessage) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	if Channel(msg.Arguments.Channel) != TickersChannel {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid channel %q", msg.Arguments.Channel)
	}

	timestamp := time.Now().UTC()
	for _, instrument := range msg.Data {
		ticker, ok := h.cache.Lookup(instrument.InstrumentID)
		if !ok {
			h.logger.Debug("received push for an unsupported instrument", "instrument", instrument.InstrumentID)
			continue
		}

		price, err := sidecartypes.ParsePrice(instrument.Last)
		if err != nil {
			unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
			continue
		}

		resolved[ticker] = types.NewResult(price, timestamp)
	}

	return types.NewResponse(resolved, unresolved), nil
}
