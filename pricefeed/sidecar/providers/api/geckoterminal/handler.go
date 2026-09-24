// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/geckoterminal/api_handler.go.
// Modified for Ark: provider integration and response handling.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package geckoterminal

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

// Handler implements api.DataHandler for GeckoTerminal.
type Handler struct{}

// NewHandler returns a new GeckoTerminal API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers applies the configured request size, capped at what the
// endpoint prices in one request. A zero batchSize takes the cap.
func (h *Handler) BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error) {
	if batchSize <= 0 || batchSize > MaxAddressesPerRequest {
		batchSize = MaxAddressesPerRequest
	}
	return api.BatchTickers(tickers, batchSize), nil
}

// CreateURL returns the URL used to fetch the given token addresses. The
// network is part of the configured endpoint, so every ticker in a batch is
// priced on that network.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) == 0 {
		return "", errors.New("no tickers provided")
	}

	addresses := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		addresses = append(addresses, string(ticker))
	}

	return url.JoinPath(endpoint.URL, strings.Join(addresses, ","))
}

// ParseResponse parses the response from the GeckoTerminal API and returns a
// types.Response. Each of the tickers supplied gets a result or an error.
func (h *Handler) ParseResponse(tickers []types.Ticker, resp *http.Response) types.Response {
	result, err := Decode(resp)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorFailedToDecode),
		)
	}

	if result.Data.Type != ExpectedResponseType {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("expected response type %s, got %q", ExpectedResponseType, result.Data.Type),
				types.ErrorInvalidResponse,
			),
		)
	}

	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)
	requestedTickers := types.NewTickers(tickers...)

	timestamp := time.Now().UTC()
	for address, rawPrice := range result.Data.Attributes.TokenPrices {
		ticker, ok := requestedTickers.Lookup(address)
		if !ok {
			continue
		}

		price, err := sidecartypes.ParsePrice(rawPrice)
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", rawPrice, err)
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
