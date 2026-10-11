// SPDX-License-Identifier: Apache-2.0
// Adapted from Connect, providers/apis/coinbase/utils.go.
// Modified for Ark: response types and provider package integration.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package coinbase

import (
	"encoding/json"
	"net/http"
)

type (
	// Response is the Coinbase spot price envelope. See README.md for the
	// wire shape.
	Response struct {
		Data Data `json:"data"`
		// Errors is set instead of Data when Coinbase refuses the request.
		Errors []APIError `json:"errors"`
	}

	// APIError is one entry of Coinbase's error envelope.
	APIError struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}

	// Data carries the fields this adapter reads from one spot price.
	Data struct {
		// Amount is the spot price in the quote currency.
		Amount string `json:"amount"`
		// Base is the currency priced.
		Base string `json:"base"`
		// Currency is the quote currency.
		Currency string `json:"currency"`
	}
)

// Decode decodes the given HTTP response into a Coinbase spot price response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	err := json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}
