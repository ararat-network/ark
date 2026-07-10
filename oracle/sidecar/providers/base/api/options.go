package api

import (
	"cosmossdk.io/log/v2"

	"ark/oracle/sidecar/providers/types"
)

// Option configures an API fetcher.
type Option func(*Fetcher)

// WithHTTPMethod is an option that is used to set the HTTP method used to make requests.
func WithHTTPMethod(method string) Option {
	return func(r *Fetcher) {
		r.method = method
	}
}

// WithHTTPHeaders is an option that is used to set the HTTP headers used to make requests.
func WithHTTPHeaders(headers map[string]string) Option {
	return func(r *Fetcher) {
		r.headers = headers
	}
}

// WithLogger sets the fetcher logger.
func WithLogger(logger log.Logger) Option {
	return func(f *Fetcher) {
		f.logger = logger
	}
}

// WithEndpointSelector sets the endpoint selection strategy used for each request.
func WithEndpointSelector(selector types.EndpointSelector) Option {
	return func(f *Fetcher) {
		f.endpointSelector = selector
	}
}
