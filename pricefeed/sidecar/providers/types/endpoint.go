package types

import (
	"errors"
	"fmt"
	"net/url"
)

// Endpoint describes a provider endpoint that a fetcher can connect to.
type Endpoint struct {
	// URL is the endpoint URL used by the fetcher.
	URL string `mapstructure:"url"`

	// Authentication holds optional endpoint authentication data.
	Authentication Authentication `mapstructure:"authentication"`
}

// Validate performs validation of the provider endpoint.
func (e Endpoint) Validate() error {
	if e.URL == "" {
		return errors.New("endpoint url cannot be empty")
	}

	u, err := url.Parse(e.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return errors.New("endpoint must be an absolute URL without user information or a fragment")
	}
	if u.Scheme != "https" && u.Scheme != "wss" {
		return errors.New("provider endpoint requires https or wss")
	}
	return e.Authentication.Validate()
}

// ValidateScheme checks URL structure, authentication, and the fetcher's protocol.
func (e Endpoint) ValidateScheme(scheme string) error {
	if err := e.Validate(); err != nil {
		return err
	}
	u, _ := url.Parse(e.URL) // Validate parsed it above.
	if u.Scheme != scheme {
		return fmt.Errorf("provider endpoint requires %s", scheme)
	}
	return nil
}

// Authentication holds optional endpoint authentication data.
type Authentication struct {
	// APIKey is the key sent to the provider when authentication is enabled.
	APIKey string `mapstructure:"api_key"`

	// APIKeyHeader is the header used to send the API key.
	APIKeyHeader string `mapstructure:"api_key_header"`
}

// Enabled returns true if the authentication is fully configured.
func (a Authentication) Enabled() bool {
	return a.APIKey != "" && a.APIKeyHeader != ""
}

// Validate performs validation of the endpoint authentication.
func (a Authentication) Validate() error {
	if a.APIKey != "" && a.APIKeyHeader == "" {
		return errors.New("api key header cannot be empty when api key is set")
	}

	if a.APIKey == "" && a.APIKeyHeader != "" {
		return errors.New("api key cannot be empty when api key header is set")
	}

	return nil
}

// EndpointSelector chooses the configured endpoint for an attempt. attempt
// counts the failures behind it, so a selector can move past an endpoint that
// failed.
type EndpointSelector func(endpoints []Endpoint, attempt int) (Endpoint, error)

// RotateEndpoints returns the endpoints in configured order, one further on for
// each failed attempt, wrapping round: the first until it fails, then the next.
func RotateEndpoints(endpoints []Endpoint, attempt int) (Endpoint, error) {
	if len(endpoints) == 0 {
		return Endpoint{}, errors.New("endpoints cannot be empty")
	}
	if attempt < 0 {
		attempt = 0
	}

	return endpoints[attempt%len(endpoints)], nil
}
