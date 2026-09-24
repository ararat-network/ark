// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/coingecko/api_handler.go.
// Modified for Ark: provider integration and response handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coingecko

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

// Handler implements api.DataHandler for CoinGecko.
type Handler struct{}

// NewHandler returns a new CoinGecko API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers applies the configured maximum request size. A zero batchSize
// keeps all tickers in one request.
func (h *Handler) BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error) {
	return api.BatchTickers(tickers, batchSize), nil
}

// CreateURL returns the URL used to fetch the given tickers. The endpoint
// prices every requested coin id against every requested quote currency, so
// the request carries the unique ids and quotes and the parser drops the
// combinations nobody asked for.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	ids, quotes, err := uniqueIDsAndQuotes(tickers)
	if err != nil {
		return "", err
	}

	parsedURL, err := url.Parse(endpoint.URL)
	if err != nil {
		return "", fmt.Errorf("parsing CoinGecko endpoint: %w", err)
	}
	query := parsedURL.Query()
	query.Set("ids", strings.Join(ids, ","))
	query.Set("vs_currencies", strings.Join(quotes, ","))
	query.Set("precision", Precision)
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

// ParseResponse parses the response from the CoinGecko API and returns a
// types.Response. Each of the tickers supplied gets a result or an error.
func (h *Handler) ParseResponse(tickers []types.Ticker, resp *http.Response) types.Response {
	result, err := Decode(resp)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorFailedToDecode),
		)
	}

	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)
	requestedTickers := types.NewTickers(tickers...)

	timestamp := time.Now().UTC()
	for id, quotes := range result {
		for quote, rawPrice := range quotes {
			ticker, ok := requestedTickers.Lookup(id + TickerSeparator + quote)
			if !ok {
				continue
			}

			price, err := sidecartypes.ParsePrice(rawPrice.String())
			if err != nil {
				wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", rawPrice.String(), err)
				unresolved[ticker] = types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice)
				continue
			}

			resolved[ticker] = types.NewResult(price, timestamp)
		}
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

// uniqueIDsAndQuotes splits each ticker into its coin id and quote currency
// and returns each list lower-cased, as CoinGecko keys them, without
// duplicates and in first-seen order. The parser's lookup ignores case, so
// the configured spelling still resolves.
func uniqueIDsAndQuotes(tickers []types.Ticker) ([]string, []string, error) {
	if len(tickers) == 0 {
		return nil, nil, errors.New("no tickers provided")
	}

	seenIDs := make(map[string]struct{})
	ids := make([]string, 0, len(tickers))
	seenQuotes := make(map[string]struct{})
	quotes := make([]string, 0, len(tickers))

	for _, ticker := range tickers {
		id, quote, ok := strings.Cut(string(ticker), TickerSeparator)
		if !ok || id == "" || quote == "" {
			return nil, nil, fmt.Errorf("ticker %q is not in id/quote form", ticker)
		}
		id, quote = strings.ToLower(id), strings.ToLower(quote)

		if _, ok := seenIDs[id]; !ok {
			seenIDs[id] = struct{}{}
			ids = append(ids, id)
		}
		if _, ok := seenQuotes[quote]; !ok {
			seenQuotes[quote] = struct{}{}
			quotes = append(quotes, quote)
		}
	}

	return ids, quotes, nil
}
