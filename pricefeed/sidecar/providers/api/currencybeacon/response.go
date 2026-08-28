package currencybeacon

import (
	"encoding/json"
	"net/http"
)

// Response is the CurrencyBeacon latest-rates envelope.
type Response struct {
	Meta    Meta    `json:"meta"`
	Payload Payload `json:"response"`
}

// Meta carries the CurrencyBeacon response status.
type Meta struct {
	Code int `json:"code"`
}

// Payload is the CurrencyBeacon latest-rates payload.
type Payload struct {
	Date  string                 `json:"date"`
	Base  string                 `json:"base"`
	Rates map[string]json.Number `json:"rates"`
}

// Decode decodes the given HTTP response into a CurrencyBeacon response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	err := decoder.Decode(&result)
	return result, err
}
