package types_test

import (
	. "noah/oracle/sidecar/providers/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEndpointValidate(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    Endpoint
		errContains string
	}{
		{
			name:     "valid URL without auth",
			endpoint: Endpoint{URL: "https://provider.test"},
		},
		{
			name: "valid URL with auth",
			endpoint: Endpoint{
				URL: "https://provider.test",
				Authentication: Authentication{
					APIKey:       "secret",
					APIKeyHeader: "X-API-Key",
				},
			},
		},
		{
			name:        "missing URL",
			endpoint:    Endpoint{},
			errContains: "endpoint url cannot be empty",
		},
		{
			name: "partial auth missing header",
			endpoint: Endpoint{
				URL:            "https://provider.test",
				Authentication: Authentication{APIKey: "secret"},
			},
			errContains: "api key header cannot be empty",
		},
		{
			name: "partial auth missing key",
			endpoint: Endpoint{
				URL:            "https://provider.test",
				Authentication: Authentication{APIKeyHeader: "X-API-Key"},
			},
			errContains: "api key cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.endpoint.Validate()
			if tt.errContains == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, tt.errContains)
		})
	}
}

func TestAuthenticationEnabled(t *testing.T) {
	require.False(t, Authentication{}.Enabled())
	require.False(t, Authentication{APIKey: "secret"}.Enabled())
	require.False(t, Authentication{APIKeyHeader: "X-API-Key"}.Enabled())
	require.True(t, Authentication{APIKey: "secret", APIKeyHeader: "X-API-Key"}.Enabled())
}

func TestFirstEndpoint(t *testing.T) {
	endpoints := []Endpoint{
		{URL: "https://first.provider.test"},
		{URL: "https://second.provider.test"},
	}

	endpoint, err := FirstEndpoint(endpoints)
	require.NoError(t, err)
	require.Equal(t, endpoints[0], endpoint)

	_, err = FirstEndpoint(nil)
	require.ErrorContains(t, err, "endpoints cannot be empty")
}
