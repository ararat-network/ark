// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/websockets/gate/parse.go.
// Modified for Ark: subscribe response and ticker update parsing.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package gate

import (
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parseSubscribeResponse checks that a subscription succeeded.
func (h *Handler) parseSubscribeResponse(msg SubscribeResponse) error {
	if msg.Error != nil {
		return fmt.Errorf("gate error %d (%s): %w", msg.Error.Code, msg.Error.Message, ErrorCode(msg.Error.Code).Error())
	}
	if Status(msg.Result.Status) != StatusSuccess {
		return fmt.Errorf("subscription was not successful: %q", msg.Result.Status)
	}

	h.logger.Debug("successfully subscribed", "id", msg.ID)
	return nil
}

// parseTickerUpdate converts a tickers channel update into a provider
// response.
func (h *Handler) parseTickerUpdate(msg TickerUpdate) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	if Channel(msg.Channel) != ChannelTickers {
		return types.NewResponse(resolved, unresolved), fmt.Errorf("invalid channel %q", msg.Channel)
	}

	ticker, ok := h.cache.Lookup(msg.Result.CurrencyPair)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", msg.Result.CurrencyPair)
	}

	price, err := sidecartypes.ParsePrice(msg.Result.Last)
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(price, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}
