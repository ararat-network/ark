package binance

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	api "noah/oracle/sidecar/providers/base/api"
	"noah/oracle/sidecar/providers/types"
	oracletypes "noah/oracle/sidecar/types"
)

var _ api.DataHandler = (*Handler)(nil)

// Handler implements api.DataHandler for Binance.
// For more information about the Binance API, refer to the following link:
// https://github.com/binance/binance-spot-api-docs/blob/master/rest-api.md#public-api-endpoints
type Handler struct{}

// NewHandler returns a new Binance API data handler.
func NewHandler() api.DataHandler {
	return &Handler{}
}

// CreateURL returns the URL that is used to fetch data from the Binance API for the
// given tickers.
func (h *Handler) CreateURL(
	endpoint types.Endpoint,
	tickers []types.Ticker,
) (string, error) {
	var tickerStrings string
	for _, ticker := range tickers {
		tickerStrings += fmt.Sprintf("%s%s%s%s", Quotation, ticker, Quotation, Separator)
	}

	if len(tickerStrings) == 0 {
		return "", errors.New("empty url created. invalid or no ticker were provided")
	}

	return fmt.Sprintf(
		endpoint.URL,
		LeftBracket,
		strings.TrimSuffix(tickerStrings, Separator),
		RightBracket,
	), nil
}

// ParseResponse parses the response from the Binance API and returns a types.Response. Each
// of the tickers supplied will get a response or an error.
func (h *Handler) ParseResponse(
	tickers []types.Ticker,
	resp *http.Response,
) types.Response {
	// Parse the response into Binance ticker prices.
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

	for _, data := range result {
		// Filter out the responses that are not expected.
		ticker, ok := requestedTickers.Lookup(data.Symbol)
		if !ok {
			continue
		}

		price, err := oracletypes.ParsePrice(data.Price)
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", data.Price, err)
			unresolved[ticker] = types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice)
			continue
		}

		resolved[ticker] = types.NewResult(price, time.Now().UTC())
	}

	// Add currency pairs that received no response to the unresolved map.
	for _, ticker := range tickers {
		_, resolvedOk := resolved[ticker]
		_, unresolvedOk := unresolved[ticker]

		if !resolvedOk && !unresolvedOk {
			unresolved[ticker] = types.NewErrorWithCode(errors.New("no response"), types.ErrorNoResponse)
		}
	}

	return types.NewResponse(resolved, unresolved)
}
