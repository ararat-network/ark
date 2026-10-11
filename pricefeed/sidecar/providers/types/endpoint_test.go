package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
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

func TestRotateEndpoints(t *testing.T) {
	endpoints := []Endpoint{
		{URL: "https://first.provider.test"},
		{URL: "https://second.provider.test"},
	}

	for _, tc := range []struct {
		attempt int
		want    Endpoint
	}{
		{attempt: 0, want: endpoints[0]},
		{attempt: 1, want: endpoints[1]},
		{attempt: 2, want: endpoints[0]},
		{attempt: -1, want: endpoints[0]},
	} {
		endpoint, err := RotateEndpoints(endpoints, tc.attempt)
		require.NoError(t, err)
		require.Equal(t, tc.want, endpoint, "attempt %d", tc.attempt)
	}

	_, err := RotateEndpoints(nil, 0)
	require.ErrorContains(t, err, "endpoints cannot be empty")
}

func TestEndpointRequiresSecureTransport(t *testing.T) {
	for _, tc := range []struct {
		url, scheme string
		bad         bool
	}{
		{"https://provider.example/prices", "https", false},
		{"wss://provider.example/prices", "wss", false},
		{"http://provider.example/prices", "https", true},
		{"ws://provider.example/prices", "wss", true},
		{"wss://provider.example/prices", "https", true},
		{"https://provider.example/prices", "wss", true},
		{"//provider.example/prices", "https", true},
		{"https:///prices", "https", true},
		{"https://user:secret@provider.example/prices", "https", true},
		{"https://provider.example/prices#fragment", "https", true},
	} {
		t.Run(tc.url+" as "+tc.scheme, func(t *testing.T) {
			require.Equal(t, tc.bad, (Endpoint{URL: tc.url}).ValidateScheme(tc.scheme) != nil)
		})
	}
}
