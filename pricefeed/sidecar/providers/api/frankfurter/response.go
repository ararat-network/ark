package frankfurter

import (
	"encoding/json"
	"net/http"
)

// Response is one rate returned by the Frankfurter latest-rates endpoint.
type Response struct {
	Date  string      `json:"date"`
	Base  string      `json:"base"`
	Quote string      `json:"quote"`
	Rate  json.Number `json:"rate"`
}

// Decode decodes the given HTTP response into Frankfurter rate responses.
func Decode(resp *http.Response) ([]Response, error) {
	var result []Response
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	err := decoder.Decode(&result)
	return result, err
}
