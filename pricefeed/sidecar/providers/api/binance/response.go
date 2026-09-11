// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/binance/utils.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package binance

import (
	"encoding/json"
	"net/http"
)

type (
	// Response is the Binance ticker-price array of symbol/price objects. See README.md for the
	// request and response shape.
	Response []Data

	// Data BinanceData is the data returned by the Binance API.
	Data struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
)

// Decode decodes the given http response into a BinanceResponse.
func Decode(resp *http.Response) (Response, error) {
	// Parse the response into a BinanceResponse.
	var result Response
	err := json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
