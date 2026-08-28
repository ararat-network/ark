package websocket

import (
	"net/http"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// Option configures a websocket fetcher.
type Option func(*Fetcher)

// WithLogger sets the fetcher logger.
func WithLogger(logger log.Logger) Option {
	return func(f *Fetcher) {
		f.logger = logger
	}
}

// WithDialFunc sets the websocket dial function.
func WithDialFunc(dial DialFunc) Option {
	return func(f *Fetcher) {
		f.dial = dial
	}
}

// WithHTTPClient sets the HTTP client used by the websocket dialer.
func WithHTTPClient(client *http.Client) Option {
	return func(f *Fetcher) {
		f.httpClient = client
	}
}

// WithHeaders sets the headers sent during websocket dial.
func WithHeaders(headers http.Header) Option {
	return func(f *Fetcher) {
		f.headers = headers
	}
}

// WithEndpointSelector sets the endpoint selection strategy used for each connection attempt.
func WithEndpointSelector(selector types.EndpointSelector) Option {
	return func(f *Fetcher) {
		f.endpointSelector = selector
	}
}
