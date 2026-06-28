package types

import "fmt"

// Endpoint describes a provider endpoint that a fetcher can connect to.
type Endpoint struct {
	// URL is the endpoint URL used by the fetcher.
	URL string `json:"url"`

	// Authentication holds optional endpoint authentication data.
	Authentication Authentication `json:"authentication"`
}

// Validate performs validation of the provider endpoint.
func (e Endpoint) Validate() error {
	if e.URL == "" {
		return fmt.Errorf("endpoint url cannot be empty")
	}

	return e.Authentication.Validate()
}

// Authentication holds optional endpoint authentication data.
type Authentication struct {
	// APIKey is the key sent to the provider when authentication is enabled.
	APIKey string `json:"apiKey"`

	// APIKeyHeader is the header used to send the API key.
	APIKeyHeader string `json:"apiKeyHeader"`
}

// Enabled returns true if the authentication is fully configured.
func (a Authentication) Enabled() bool {
	return a.APIKey != "" && a.APIKeyHeader != ""
}

// Validate performs validation of the endpoint authentication.
func (a Authentication) Validate() error {
	if a.APIKey != "" && a.APIKeyHeader == "" {
		return fmt.Errorf("api key header cannot be empty when api key is set")
	}

	if a.APIKey == "" && a.APIKeyHeader != "" {
		return fmt.Errorf("api key cannot be empty when api key header is set")
	}

	return nil
}

// EndpointSelector chooses one configured provider endpoint for an attempt.
type EndpointSelector func(endpoints []Endpoint) (Endpoint, error)

// FirstEndpoint returns the first configured provider endpoint.
func FirstEndpoint(endpoints []Endpoint) (Endpoint, error) {
	if len(endpoints) == 0 {
		return Endpoint{}, fmt.Errorf("endpoints cannot be empty")
	}

	return endpoints[0], nil
}
