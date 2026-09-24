// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/geckoterminal/utils.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package geckoterminal

import (
	"encoding/json"
	"net/http"
)

type (
	// Response is the GeckoTerminal token price envelope. See README.md for
	// the wire shape.
	Response struct {
		Data Data `json:"data"`
	}

	// Data is the typed payload; Type must be ExpectedResponseType.
	Data struct {
		Type       string     `json:"type"`
		Attributes Attributes `json:"attributes"`
	}

	// Attributes carries the token prices keyed by contract address. Prices
	// are decimal strings.
	Attributes struct {
		TokenPrices map[string]string `json:"token_prices"`
	}
)

// Decode decodes the given HTTP response into a GeckoTerminal token price
// response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	err := json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
