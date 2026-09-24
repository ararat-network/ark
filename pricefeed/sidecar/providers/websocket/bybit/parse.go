// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/bybit/parse.go.
// Modified for Ark: ticker update parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bybit

import (
	"fmt"
	"strings"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseTickerUpdate converts a tickers topic update into a provider response.
func (h *Handler) parseTickerUpdate(msg TickerUpdateMessage) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	if !strings.HasPrefix(msg.Topic, string(TickerChannel)+TopicSeparator) {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid topic %q", msg.Topic)
	}

	ticker, ok := h.cache.Lookup(msg.Data.Symbol)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", msg.Data.Symbol)
	}

	price, err := sidecartypes.ParsePrice(msg.Data.LastPrice)
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}
