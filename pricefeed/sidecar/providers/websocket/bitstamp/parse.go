// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bitstamp/parse.go.
// Modified for Ark: trade message parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitstamp

import (
	"fmt"
	"strings"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseTradeMessage converts a live trades message into a provider response.
// The market is the channel name after the live trades prefix.
func (h *Handler) parseTradeMessage(msg TradeMessage) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	symbol, ok := strings.CutPrefix(msg.Channel, string(TradeChannelPrefix))
	if !ok || symbol == "" {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid trade channel %q", msg.Channel)
	}

	ticker, ok := h.cache.Lookup(symbol)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", symbol)
	}

	price, err := sidecartypes.ParsePrice(msg.Data.PriceStr)
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}
