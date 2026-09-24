// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/coingecko/utils.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coingecko

import (
	"encoding/json"
	"net/http"
)

// Response maps each coin id to its price per quote currency. Prices are kept
// as their decimal text rather than float64. See README.md for the wire shape.
type Response map[string]map[string]json.Number

// Decode decodes the given HTTP response into a CoinGecko simple price
// response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	err := decoder.Decode(&result)
	return result, err
}
