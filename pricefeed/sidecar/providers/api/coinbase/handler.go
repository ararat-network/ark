// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/coinbase/api_handler.go.
// Modified for Ark: provider integration and response handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coinbase

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	api "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

var _ api.DataHandler = (*Handler)(nil)

// Handler implements api.DataHandler for Coinbase.
type Handler struct{}

// NewHandler returns a new Coinbase API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers puts every ticker in its own request. The spot endpoint
// prices one product per request, so the configured batch size is not used.
func (h *Handler) BatchTickers(tickers []types.Ticker, _ int) ([][]types.Ticker, error) {
	return api.BatchTickers(tickers, 1), nil
}

// CreateURL returns the spot price URL for one product.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) != 1 {
		return "", fmt.Errorf("expected 1 ticker, got %d", len(tickers))
	}
	if _, err := quoteCurrency(tickers[0]); err != nil {
		return "", err
	}

	return url.JoinPath(endpoint.URL, string(tickers[0]), SpotSegment)
}

// ParseResponse parses the spot price response for the single requested
// product. The response must be quoted in the product's quote currency.
func (h *Handler) ParseResponse(tickers []types.Ticker, resp *http.Response) types.Response {
	if len(tickers) != 1 {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("expected 1 ticker, got %d", len(tickers)),
				types.ErrorInvalidResponse,
			),
		)
	}

	result, err := Decode(resp)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorFailedToDecode),
		)
	}

	ticker := tickers[0]
	quote, err := quoteCurrency(ticker)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorUnknownPair),
		)
	}
	if !strings.EqualFold(result.Data.Currency, quote) {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("spot price for %s is quoted in %q, expected %s", ticker, result.Data.Currency, quote),
				types.ErrorInvalidResponse,
			),
		)
	}
	if result.Data.Amount == "" {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(errors.New("no response"), types.ErrorNoResponse),
		)
	}

	price, err := sidecartypes.ParsePrice(result.Data.Amount)
	if err != nil {
		wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", result.Data.Amount, err)
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice),
		)
	}

	return types.NewResponse(
		map[types.Ticker]types.Result{ticker: types.NewResult(price, time.Now().UTC())},
		nil,
	)
}

// quoteCurrency returns the quote of a BASE-QUOTE product.
func quoteCurrency(ticker types.Ticker) (string, error) {
	base, quote, ok := strings.Cut(string(ticker), ProductSeparator)
	if !ok || base == "" || quote == "" {
		return "", fmt.Errorf("ticker %q is not in BASE-QUOTE form", ticker)
	}
	return quote, nil
}
