package binance

import (
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parsePriceUpdateMessage converts a Binance symbol and price string into a
// provider response. It is shared by ticker and aggregate trade stream
// messages, which arrive on separate streams: a message whose last trade is
// behind one already published is dropped, so a ticker push cannot roll a
// newer trade's price back.
func (h *Handler) parsePriceUpdateMessage(rawTicker string, price string, lastTradeID int64) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	ticker, ok := h.cache.Lookup(rawTicker)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", rawTicker)
	}
	if seen, ok := h.lastTrades[ticker]; ok && lastTradeID < seen {
		return types.NewResponse(resolved, unresolved), nil
	}

	// Convert the price to a big Float.
	priceFloat, err := sidecartypes.ParsePrice(price)
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	h.lastTrades[ticker] = lastTradeID
	resolved[ticker] = types.NewResult(priceFloat, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}

// refusedResponse publishes the tickers of a refused subscription unresolved.
func (h *Handler) refusedResponse(instruments []string, refusal ErrorResponse) types.Response {
	unresolved := make(map[types.Ticker]types.ErrorWithCode, len(instruments))
	for _, instrument := range instruments {
		ticker, ok := h.cache.Lookup(instrument)
		if !ok {
			continue
		}
		unresolved[ticker] = types.NewErrorWithCode(
			fmt.Errorf("binance refused the subscription: %d %s", refusal.Code, refusal.Message),
			types.ErrorInvalidWebSocketTopic,
		)
	}

	return types.NewResponse(nil, unresolved)
}
