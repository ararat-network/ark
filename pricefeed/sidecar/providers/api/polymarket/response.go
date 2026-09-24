// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/polymarket/api_handler.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package polymarket

import (
	"encoding/json"
	"net/http"
)

type (
	// Response is the market object returned for one condition id. Only the
	// outcome tokens are read. See README.md for the wire shape.
	Response struct {
		Tokens []Token `json:"tokens"`
	}

	// Token is one outcome token of a market. The price is kept as its
	// decimal text rather than float64.
	Token struct {
		TokenID string      `json:"token_id"`
		Outcome string      `json:"outcome"`
		Price   json.Number `json:"price"`
	}
)

// Decode decodes the given HTTP response into a Polymarket market response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	err := decoder.Decode(&result)
	return result, err
}
