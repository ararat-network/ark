// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/kraken/api_handler.go.
// Modified for Ark: provider integration and response handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kraken

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

// Handler implements api.DataHandler for Kraken.
type Handler struct{}

// NewHandler returns a new Kraken API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers applies the configured maximum request size. A zero batchSize
// keeps all tickers in one request.
func (h *Handler) BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error) {
	return api.BatchTickers(tickers, batchSize), nil
}

// CreateURL returns the URL used to fetch the given pairs from the Kraken API.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) == 0 {
		return "", errors.New("no tickers provided")
	}

	parsedURL, err := url.Parse(endpoint.URL)
	if err != nil {
		return "", fmt.Errorf("parsing Kraken endpoint: %w", err)
	}
	pairs := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		pairs = append(pairs, string(ticker))
	}
	query := parsedURL.Query()
	query.Set("pair", strings.Join(pairs, Separator))
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

// ParseResponse parses the response from the Kraken API and returns a
// types.Response. Each of the tickers supplied gets a result or an error.
func (h *Handler) ParseResponse(tickers []types.Ticker, resp *http.Response) types.Response {
	result, err := Decode(resp)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorFailedToDecode),
		)
	}

	// A request-level failure fills error and leaves result empty.
	if len(result.Errors) > 0 {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("kraken error: %s", strings.Join(result.Errors, ", ")),
				types.ErrorAPIGeneral,
			),
		)
	}

	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)
	requestedTickers := types.NewTickers(tickers...)

	timestamp := time.Now().UTC()
	for pair, data := range result.Tickers {
		ticker, ok := requestedTickers.Lookup(pair)
		if !ok {
			continue
		}
		if len(data.Close) == 0 {
			unresolved[ticker] = types.NewErrorWithCode(errors.New("no close price"), types.ErrorInvalidResponse)
			continue
		}

		price, err := sidecartypes.ParsePrice(data.Close[0])
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", data.Close[0], err)
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
