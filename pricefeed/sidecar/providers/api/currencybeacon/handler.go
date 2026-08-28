package currencybeacon

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/api/internal/fiat"
	api "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	oracletypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

var _ api.DataHandler = (*Handler)(nil)

// Handler implements api.DataHandler for CurrencyBeacon.
type Handler struct{}

// NewHandler returns a new CurrencyBeacon API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers groups tickers by base currency, then applies batchSize within
// each base group. A zero batchSize keeps each base group in one request.
func (h *Handler) BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error) {
	return fiat.BatchTickers(tickers, batchSize)
}

// CreateURL returns the URL used to fetch one same-base exchange-rate batch.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	base, quotes, err := fiat.BatchBaseQuotes(tickers)
	if err != nil {
		return "", err
	}

	parsedURL, err := url.Parse(endpoint.URL)
	if err != nil {
		return "", fmt.Errorf("parsing CurrencyBeacon endpoint: %w", err)
	}
	query := parsedURL.Query()
	query.Set("base", base)
	query.Set("symbols", strings.Join(quotes, ","))
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

// ParseResponse parses the response from the CurrencyBeacon API and returns a
// types.Response.
func (h *Handler) ParseResponse(tickers []types.Ticker, resp *http.Response) types.Response {
	result, err := Decode(resp)
	if err != nil {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(err, types.ErrorFailedToDecode),
		)
	}
	if result.Meta.Code != http.StatusOK {
		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				fmt.Errorf("CurrencyBeacon response code %d", result.Meta.Code),
				types.ErrorAPIGeneral,
			),
		)
	}

	resolved := make(map[types.Ticker]types.Result)
	unresolved := make(map[types.Ticker]types.ErrorWithCode)
	requestedTickers := types.NewTickers(tickers...)

	timestamp := time.Now().UTC()
	for quote, rate := range result.Payload.Rates {
		ticker, ok := requestedTickers.Lookup(fiat.PairKey(result.Payload.Base, quote))
		if !ok {
			continue
		}
		price, err := oracletypes.ParsePrice(rate.String())
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", rate.String(), err)
			unresolved[ticker] = types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice)
		} else {
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
