package frankfurter

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	api "noah/oracle/sidecar/providers/base/api"
	"noah/oracle/sidecar/providers/types"
	oracletypes "noah/oracle/sidecar/types"
)

var _ api.DataHandler = (*Handler)(nil)

// Handler implements api.DataHandler for Frankfurter.
type Handler struct{}

// NewHandler returns a new Frankfurter API data handler.
func NewHandler() api.DataHandler {
	return &Handler{}
}

// CreateURL returns the URL used to fetch a single exchange-rate pair.
func (h *Handler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if len(tickers) != 1 {
		return "", fmt.Errorf("expected exactly one ticker, got %d", len(tickers))
	}

	base, quote, err := splitTicker(tickers[0])
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(endpoint.URL, url.PathEscape(base), url.PathEscape(quote)), nil
}

// ParseResponse parses the response from the Frankfurter API and returns a types.Response.
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

	ticker, ok := requestedTickers.Lookup(pairKey(result.Base, result.Quote))
	if ok {
		price, err := oracletypes.ParsePrice(result.Rate.String())
		if err != nil {
			wErr := fmt.Errorf("failed to convert price %s to big.Float: %w", result.Rate.String(), err)
			unresolved[ticker] = types.NewErrorWithCode(wErr, types.ErrorFailedToParsePrice)
		} else {
			resolved[ticker] = types.NewResult(price, time.Now().UTC())
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
