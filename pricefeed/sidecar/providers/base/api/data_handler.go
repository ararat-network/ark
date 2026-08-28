package api

import (
	"net/http"

	"ark/pricefeed/sidecar/providers/types"
)

// DataHandler contains provider-specific API behaviour used by Fetcher.
// It builds request URLs for ticker batches and parses successful HTTP
// responses into provider responses.
type DataHandler interface {
	// BatchTickers groups provider tickers into independently fetched requests.
	BatchTickers(tickers []types.Ticker, batchSize int) ([][]types.Ticker, error)

	// CreateURL builds the request URL for a batch of provider tickers.
	CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error)

	// ParseResponse parses a successful HTTP response into resolved and unresolved
	// ticker results. Parse failures should be represented in the returned response's
	// unresolved map, not returned as Go errors.
	ParseResponse(tickers []types.Ticker, response *http.Response) types.Response
}
