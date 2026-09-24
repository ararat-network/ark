// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/coinmarketcap/api_handler.go.
// Modified for Ark: provider integration and response handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coinmarketcap

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

// Handler implements api.DataHandler for CoinMarketCap.
type Handler struct{}

// NewHandler returns a new CoinMarketCap API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers applies the configured maximum request size. A zero batchSize
// keeps all tickers in one request.
func (h *Handler) BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error) {
	return api.BatchTickers(tickers, batchSize), nil
}

// CreateURL returns the URL used to fetch the given ids from the CoinMarketCap
// API.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) == 0 {
		return "", errors.New("no tickers provided")
	}

	parsedURL, err := url.Parse(endpoint.URL)
	if err != nil {
		return "", fmt.Errorf("parsing CoinMarketCap endpoint: %w", err)
	}
	ids := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		ids = append(ids, string(ticker))
	}
	query := parsedURL.Query()
	query.Set("id", strings.Join(ids, ","))
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

// ParseResponse parses the response from the CoinMarketCap API and returns a
// types.Response. Each of the tickers supplied gets a result or an error.
func (h *Handler) ParseResponse(tickers []types.Ticker, resp *http.Response) types.Response {
	result, err := Decode(resp)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorFailedToDecode),
		)
	}

	if result.Status.ErrorCode != 0 {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("coinmarketcap error %d: %s", result.Status.ErrorCode, result.Status.ErrorMessage),
				types.ErrorAPIGeneral,
			),
		)
	}

	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)
	requestedTickers := types.NewTickers(tickers...)

	timestamp := time.Now().UTC()
	for id, data := range result.Data {
		ticker, ok := requestedTickers.Lookup(id)
		if !ok {
			continue
		}

		quote, ok := data.Quote[QuoteCurrency]
		if !ok {
			unresolved[ticker] = types.NewErrorWithCode(
				fmt.Errorf("no %s quote for id %s", QuoteCurrency, id),
				types.ErrorNoResponse,
			)
			continue
		}

		price, err := sidecartypes.ParsePrice(quote.Price.String())
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", quote.Price.String(), err)
			unresolved[ticker] = types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice)
			continue
		}

		resolved[ticker] = types.NewResult(price, timestamp)
	}

	for _, ticker := range tickers {
		_, resolvedOk := resolved[ticker]
		_, unresolvedOk := unresolved[ticker]

		if !resolvedOk && !unresolvedOk {
			unresolved[ticker] = types.NewErrorWithCode(errors.New("no response"), types.ErrorNoResponse)
		}
	}

	return types.NewResponse(resolved, unresolved)
}
