package frankfurter

import (
	"encoding/json"
	"net/http"
)

// Response is the expected response returned by the Frankfurter single-pair rate endpoint.
type Response struct {
	Date  string      `json:"date"`
	Base  string      `json:"base"`
	Quote string      `json:"quote"`
	Rate  json.Number `json:"rate"`
}

// Decode decodes the given HTTP response into a Frankfurter response.
func Decode(resp *http.Response) (Response, error) {
	var result Response
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()

	err := decoder.Decode(&result)
	return result, err
}
