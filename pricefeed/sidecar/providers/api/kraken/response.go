// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/kraken/utils.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package kraken

import (
	"encoding/json"
	"net/http"
)

type (
	// Response is the Kraken ticker envelope: request-level errors plus results
	// keyed by full pair name. See README.md for the wire shape.
	Response struct {
		Errors  []string              `json:"error"`
		Tickers map[string]TickerData `json:"result"`
	}

	// TickerData carries the fields this adapter reads from one pair's ticker.
	TickerData struct {
		// Close is the last trade closed array: price, then lot volume.
		Close []string `json:"c"`
	}
)

// Decode decodes the given HTTP response into a Kraken ticker response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	err := json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
