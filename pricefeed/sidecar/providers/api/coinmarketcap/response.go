// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/coinmarketcap/utils.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coinmarketcap

import (
	"encoding/json"
	"net/http"
)

type (
	// Response is the CoinMarketCap quotes envelope: per-id data plus a
	// request status. See README.md for the wire shape.
	Response struct {
		Data   map[string]Data `json:"data"`
		Status Status          `json:"status"`
	}

	// Data carries the fields this adapter reads from one asset.
	Data struct {
		// Quote maps quote currency to its quote.
		Quote map[string]Quote `json:"quote"`
	}

	// Quote is one asset's quote in one currency. The price is kept as its
	// decimal text rather than float64.
	Quote struct {
		Price json.Number `json:"price"`
	}

	// Status is the request status. A non-zero error code fails the request.
	Status struct {
		ErrorCode    int64  `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	}
)

// Decode decodes the given HTTP response into a CoinMarketCap quotes response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	err := decoder.Decode(&result)
	return result, err
}
