// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/bitstamp/api_handler.go.
// Modified for Ark: provider integration and response handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitstamp

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	api "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

var _ api.DataHandler = (*Handler)(nil)

// Handler implements api.DataHandler for Bitstamp.
type Handler struct{}

// NewHandler returns a new Bitstamp API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers keeps every ticker in one request. The ticker endpoint returns
// all markets in one response, so a smaller batch would only repeat the same
// request.
func (h *Handler) BatchTickers(tickers []types.Ticker, _ int) ([][]types.Ticker, error) {
	if len(tickers) == 0 {
		return nil, nil
	}
	return [][]types.Ticker{tickers}, nil
}

// CreateURL returns the configured endpoint; the response is filtered to the
// requested tickers when it is parsed.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) == 0 {
		return "", errors.New("no tickers provided")
	}
	return endpoint.URL, nil
}

// ParseResponse parses the response from the Bitstamp API and returns a
// types.Response. The response carries every market; only requested pairs are
// resolved.
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
	for _, data := range result {
		ticker, ok := requestedTickers.Lookup(data.Pair)
		if !ok {
			continue
		}

		price, err := sidecartypes.ParsePrice(data.Last)
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", data.Last, err)
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
