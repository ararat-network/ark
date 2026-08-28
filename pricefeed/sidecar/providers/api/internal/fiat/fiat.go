// Package fiat holds the request conventions shared by fiat exchange-rate
// handlers: "BASE/QUOTE" tickers grouped into same-base request batches.
package fiat

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// SplitTicker splits ticker into its normalised base and quote currencies.
func SplitTicker(ticker types.Ticker) (string, string, error) {
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

// PairKey returns the normalised "BASE/QUOTE" lookup key for a rate.
func PairKey(base, quote string) string {
	return strings.ToUpper(strings.TrimSpace(base)) + "/" + strings.ToUpper(strings.TrimSpace(quote))
}

// BatchTickers groups tickers by base currency, then applies batchSize within
// each base group. A zero batchSize keeps each base group in one request.
func BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error) {
	if len(tickers) == 0 {
		return nil, nil
	}

	baseOrder := make([]string, 0)
	byBase := make(map[string][]types.Ticker)
	for _, ticker := range tickers {
		base, _, err := SplitTicker(ticker)
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

// BatchBaseQuotes returns the shared base and ordered quotes of one same-base
// batch, rejecting empty and mixed-base batches.
func BatchBaseQuotes(tickers []types.Ticker) (string, []string, error) {
	if len(tickers) == 0 {
		return "", nil, errors.New("tickers cannot be empty")
	}

	base, firstQuote, err := SplitTicker(tickers[0])
	if err != nil {
		return "", nil, err
	}
	quotes := make([]string, 0, len(tickers))
	quotes = append(quotes, firstQuote)
	for _, ticker := range tickers[1:] {
		tickerBase, quote, err := SplitTicker(ticker)
		if err != nil {
			return "", nil, err
		}
		if tickerBase != base {
			return "", nil, fmt.Errorf("ticker base %q does not match batch base %q", tickerBase, base)
		}
		quotes = append(quotes, quote)
	}

	return base, quotes, nil
}
