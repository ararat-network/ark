// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/huobi/parse.go.
// Modified for Ark: subscription response and ticker stream parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package huobi

import (
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseSubscriptionResponse checks that a subscription succeeded.
func (h *Handler) parseSubscriptionResponse(msg SubscriptionResponse) error {
	if Status(msg.Status) != StatusOK {
		return fmt.Errorf("subscription %s failed with status %q: %s", msg.ID, msg.Status, msg.ErrMsg)
	}
	if SymbolFromTopic(msg.Subbed) == "" {
		return fmt.Errorf("subscribed to an unexpected topic %q", msg.Subbed)
	}

	h.logger.Debug("successfully subscribed", "topic", msg.Subbed)
	return nil
}

// parseTickerStream converts a ticker stream into a provider response.
func (h *Handler) parseTickerStream(msg TickerStream) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	symbol := SymbolFromTopic(msg.Channel)
	if symbol == "" {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid ticker channel %q", msg.Channel)
	}

	ticker, ok := h.cache.Lookup(symbol)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", symbol)
	}

	price, err := sidecartypes.ParsePrice(msg.Tick.LastPrice.String())
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	h.observed.Add(ticker)
	return types.NewResponse(resolved, unresolved), nil
}
