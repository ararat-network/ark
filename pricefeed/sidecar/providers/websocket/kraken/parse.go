// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/kraken/parse.go.
// Modified for Ark: websocket v2 status, subscription, and ticker parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kraken

import (
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseStatusMessage checks that the platform is online.
func (h *Handler) parseStatusMessage(msg StatusMessage) error {
	if len(msg.Data) == 0 {
		return fmt.Errorf("status message carries no data")
	}
	if system := Status(msg.Data[0].System); system != SystemOnline {
		return fmt.Errorf("kraken system status is %q", system)
	}

	h.logger.Debug("system status is online")
	return nil
}

// parseSubscribeResponse checks that a subscription succeeded.
func (h *Handler) parseSubscribeResponse(msg MethodResponse) error {
	if !msg.Success {
		return fmt.Errorf("subscription to %s failed: %s", msg.Symbol, msg.Error)
	}

	h.logger.Debug("successfully subscribed", "symbol", msg.Result.Symbol)
	return nil
}

// parseTickerMessage converts a ticker push into a provider response. The
// price is the last trade, matching the API adapter; upstream's v1 handler
// read the day's volume-weighted average, which can trail the market by
// hours. Symbols this connection did not subscribe are skipped.
func (h *Handler) parseTickerMessage(msg TickerMessage) types.Response {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	timestamp := time.Now().UTC()
	for _, data := range msg.Data {
		ticker, ok := h.cache.Lookup(data.Symbol)
		if !ok {
			h.logger.Debug("received ticker for an unsupported symbol", "symbol", data.Symbol)
			continue
		}

		price, err := sidecartypes.ParsePrice(data.Last.String())
		if err != nil {
			unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
			continue
		}

		resolved[ticker] = types.NewResult(price, timestamp)
	}

	return types.NewResponse(resolved, unresolved)
}
