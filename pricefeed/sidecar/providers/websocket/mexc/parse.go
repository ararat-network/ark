// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/mexc/parse.go.
// Modified for Ark: protobuf mini ticker parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package mexc

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parsePush converts a mini ticker push into a provider response. The
// symbol is the ticker's own, falling back to the wrapper's.
func (h *Handler) parsePush(push Push) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	if !strings.HasPrefix(push.Channel, string(MiniTickerChannel)) {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid channel %q", push.Channel)
	}
	if push.MiniTicker == nil {
		return types.NewResponse(resolved, unresolved), errors.New("push frame carries no mini ticker")
	}

	symbol := push.MiniTicker.Symbol
	if symbol == "" {
		symbol = push.Symbol
	}
	ticker, ok := h.cache.Lookup(symbol)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", symbol)
	}

	price, err := sidecartypes.ParsePrice(push.MiniTicker.Price)
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}
