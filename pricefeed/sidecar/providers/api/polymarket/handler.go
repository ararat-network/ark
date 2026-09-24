// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/polymarket/api_handler.go.
// Modified for Ark: provider integration and response handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package polymarket

import (
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

// Handler implements api.DataHandler for Polymarket.
type Handler struct{}

// NewHandler returns a new Polymarket API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers puts every ticker in its own request. The markets endpoint
// describes one market per request, so the configured batch size is not used.
func (h *Handler) BatchTickers(tickers []types.Ticker, _ int) ([][]types.Ticker, error) {
	return api.BatchTickers(tickers, 1), nil
}

// CreateURL returns the market URL for one outcome token's market.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) != 1 {
		return "", fmt.Errorf("expected 1 ticker, got %d", len(tickers))
	}

	conditionID, _, err := splitTicker(tickers[0])
	if err != nil {
		return "", err
	}

	return url.JoinPath(endpoint.URL, conditionID)
}

// ParseResponse parses the market response and resolves the price of the
// single requested outcome token.
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
	ticker := tickers[0]

	result, err := Decode(resp)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorFailedToDecode),
		)
	}

	_, tokenID, err := splitTicker(ticker)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorUnknownPair),
		)
	}

	var token *Token
	for i := range result.Tokens {
		if result.Tokens[i].TokenID == tokenID {
			token = &result.Tokens[i]
			break
		}
	}
	if token == nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("token id %s not found in market response", tokenID),
				types.ErrorNoResponse,
			),
		)
	}
	rawPrice := token.Price.String()
	if rawPrice == "" {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("token id %s carries no price", tokenID),
				types.ErrorInvalidResponse,
			),
		)
	}

	price, err := sidecartypes.ParsePrice(rawPrice)
	if err != nil {
		wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", rawPrice, err)
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice),
		)
	}
	if price.Sign() == 0 {
		// MinPrice is a constant literal, so it always parses.
		price, _ = sidecartypes.ParsePrice(MinPrice)
	}

	return types.NewResponse(
		map[types.Ticker]types.Result{ticker: types.NewResult(price, time.Now().UTC())},
		nil,
	)
}

// splitTicker splits a condition_id/token_id ticker into its parts.
func splitTicker(ticker types.Ticker) (string, string, error) {
	conditionID, tokenID, ok := strings.Cut(string(ticker), TickerSeparator)
	if !ok || conditionID == "" || tokenID == "" {
		return "", "", fmt.Errorf("ticker %q is not in condition_id/token_id form", ticker)
	}
	return conditionID, tokenID, nil
}
