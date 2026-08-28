package binance

import (
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	oracletypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// parsePriceUpdateMessage converts a Binance symbol and price string into a
// provider response. It is shared by ticker and aggregate trade stream messages.
func (h *Handler) parsePriceUpdateMessage(rawTicker string, price string) (types.Response, error) {
	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)

	ticker, ok := h.cache.Lookup(rawTicker)
	if !ok {
		return types.NewResponse(resolved, unresolved),
			fmt.Errorf("got response for an unsupported market %s", rawTicker)
	}

	// Convert the price to a big Float.
	priceFloat, err := oracletypes.ParsePrice(price)
	if err != nil {
		unresolved[ticker] = types.NewErrorWithCode(err, types.ErrorFailedToParsePrice)
		return types.NewResponse(resolved, unresolved), nil
	}

	resolved[ticker] = types.NewResult(priceFloat, time.Now().UTC())
	return types.NewResponse(resolved, unresolved), nil
}
