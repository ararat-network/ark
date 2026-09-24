// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/bitstamp/utils.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package bitstamp

import (
	"encoding/json"
	"net/http"
)

type (
	// Response is the Bitstamp ticker array, one object per market. See
	// README.md for the wire shape.
	Response []Data

	// Data carries the fields this adapter reads from one market's ticker.
	Data struct {
		// Last is the last trade price.
		Last string `json:"last"`
		// Pair is the market in BASE/QUOTE form.
		Pair string `json:"pair"`
	}
)

// Decode decodes the given HTTP response into a Bitstamp ticker response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	err := json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
