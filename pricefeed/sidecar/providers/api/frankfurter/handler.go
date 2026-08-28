package frankfurter

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	api "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	oracletypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

var _ api.DataHandler = (*Handler)(nil)

// Handler implements api.DataHandler for Frankfurter.
type Handler struct{}

// NewHandler returns a new Frankfurter API data handler.
func NewHandler() *Handler {
	return &Handler{}
}

// BatchTickers groups tickers by base currency, then applies batchSize within
// each base group. A zero batchSize keeps each base group in one request.
func (h *Handler) BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error) {
	if len(tickers) == 0 {
		return nil, nil
	}

	baseOrder := make([]string, 0)
	byBase := make(map[string][]types.Ticker)
	for _, ticker := range tickers {
		base, _, err := splitTicker(ticker)
		if err != nil {
			return nil, err
		}
		if _, ok := byBase[base]; !ok {
			baseOrder = append(baseOrder, base)
		}
		byBase[base] = append(byBase[base], ticker)
	}

	batches := make([][]types.Ticker, 0, len(baseOrder))
	for _, base := range baseOrder {
		batches = append(batches, api.BatchTickers(byBase[base], batchSize)...)
	}
	return batches, nil
}

// CreateURL returns the URL used to fetch one same-base exchange-rate batch.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) == 0 {
		return "", errors.New("tickers cannot be empty")
	}

	base, firstQuote, err := splitTicker(tickers[0])
	if err != nil {
		return "", err
	}
	quotes := make([]string, 0, len(tickers))
	quotes = append(quotes, firstQuote)
	for _, ticker := range tickers[1:] {
		tickerBase, quote, err := splitTicker(ticker)
		if err != nil {
			return "", err
		}
		if tickerBase != base {
			return "", fmt.Errorf("ticker base %q does not match batch base %q", tickerBase, base)
		}
		quotes = append(quotes, quote)
	}

	parsedURL, err := url.Parse(endpoint.URL)
	if err != nil {
		return "", fmt.Errorf("parsing Frankfurter endpoint: %w", err)
	}
	query := parsedURL.Query()
	query.Set("base", base)
	query.Set("quotes", strings.Join(quotes, ","))
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

// ParseResponse parses the response from the Frankfurter API and returns a types.Response.
func (h *Handler) ParseResponse(tickers []types.Ticker, resp *http.Response) types.Response {
	results, err := Decode(resp)
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
	for _, result := range results {
		ticker, ok := requestedTickers.Lookup(pairKey(result.Base, result.Quote))
		if !ok {
			continue
		}
		price, err := oracletypes.ParsePrice(result.Rate.String())
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", result.Rate.String(), err)
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

func splitTicker(ticker types.Ticker) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(string(ticker)), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf(`expected ticker in "BASE/QUOTE" format, got %q`, ticker)
	}

	base := strings.ToUpper(strings.TrimSpace(parts[0]))
	quote := strings.ToUpper(strings.TrimSpace(parts[1]))
	if base == "" || quote == "" {
		return "", "", fmt.Errorf(`expected ticker in "BASE/QUOTE" format, got %q`, ticker)
	}

	return base, quote, nil
}

func pairKey(base, quote string) string {
	return strings.ToUpper(strings.TrimSpace(base)) + "/" + strings.ToUpper(strings.TrimSpace(quote))
}
