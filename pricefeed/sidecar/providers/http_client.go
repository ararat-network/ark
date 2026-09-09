package providers

import (
	"errors"
	"net/http"
)

// newHTTPClient uses standard certificate verification. Redirects are refused
// before a second request can carry credentials or leave the configured origin.
// WebSocket dialling receives this policy too, including after scheme conversion.
func newHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("provider redirects are refused; configure the final HTTPS or WSS endpoint")
	}}
}
