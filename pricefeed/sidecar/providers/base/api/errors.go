package api

import (
	"errors"
	"fmt"
)

var (
	// ErrCreateURL is returned when the DataHandler cannot create a URL.
	// This can occur if the provider does not have the necessary information
	// to create the URL, for example because of malformed config.
	ErrCreateURL = errors.New("api data handler failed to create URL")

	// ErrDoRequest is returned when the Fetcher cannot complete an HTTP request.
	ErrDoRequest = errors.New("api fetcher failed to make request")

	// ErrSelectEndpoint is returned when the Fetcher cannot choose an API endpoint.
	ErrSelectEndpoint = errors.New("api fetcher failed to select endpoint")

	// ErrRateLimit is returned when the Fetcher receives a rate-limit response.
	ErrRateLimit = errors.New("api fetcher encountered rate limit")

	// ErrUnexpectedStatusCode is returned when the Fetcher receives an unexpected HTTP status code.
	ErrUnexpectedStatusCode = errors.New("api fetcher received unexpected status code")
)

// ErrCreateURLWithErr wraps an underlying URL creation error.
// DataHandler implementations should use this function when URL construction fails.
func ErrCreateURLWithErr(err error) error {
	return errors.Join(ErrCreateURL, err)
}

// ErrDoRequestWithErr wraps an underlying HTTP request error.
func ErrDoRequestWithErr(err error) error {
	return errors.Join(ErrDoRequest, err)
}

// ErrSelectEndpointWithErr wraps an underlying endpoint selection error.
func ErrSelectEndpointWithErr(err error) error {
	return errors.Join(ErrSelectEndpoint, err)
}

// ErrUnexpectedStatusCodeWithCode wraps an unexpected HTTP status code.
func ErrUnexpectedStatusCodeWithCode(code int) error {
	return fmt.Errorf("%w: %d", ErrUnexpectedStatusCode, code)
}
