// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/cryptodotcom/parse.go.
// Modified for Ark: instrument update parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cryptodotcom

import (
	"errors"
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseInstrumentMessage converts a ticker update into a provider response.
// Instruments this connection did not subscribe are skipped.
func (h *Handler) parseInstrumentMessage(msg ResponseMessage) types.Response {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	timestamp := time.Now().UTC()
	for _, instrument := range msg.Result.Data {
		ticker, ok := h.cache.Lookup(instrument.Name)
		if !ok {
			h.logger.Debug("received update for an unsupported instrument", "instrument", instrument.Name)
			continue
		}

		if instrument.LatestTradePrice == "" {
			unresolved[ticker] = types.NewErrorWithCode(errors.New("instrument has no trade"), types.ErrorNoResponse)
			continue
		}

		price, err := sidecartypes.ParsePrice(instrument.LatestTradePrice)
		if err != nil {
			wErr := fmt.Errorf("failed to parse price %s: %w", instrument.LatestTradePrice, err)
			unresolved[ticker] = types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice)
			continue
		}

		resolved[ticker] = types.NewResult(price, timestamp)
	}

	return types.NewResponse(resolved, unresolved)
}
