package openexchangerates

import (
	"encoding/json"
	"net/http"
)

// Response is the Open Exchange Rates latest-rates payload.
type Response struct {
	Timestamp int64                  `json:"timestamp"`
	Base      string                 `json:"base"`
	Rates     map[string]json.Number `json:"rates"`
}

// Decode decodes the given HTTP response into an Open Exchange Rates response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	err := decoder.Decode(&result)
	return result, err
}
