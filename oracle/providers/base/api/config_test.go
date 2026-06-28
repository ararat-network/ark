package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"noah/oracle/providers/types"
)

func TestAPIConfigValidateBasicAllowsZeroOptionalControls(t *testing.T) {
	cfg := Config{
		Name:      "test",
		Endpoints: []types.Endpoint{{URL: "https://provider.test"}},
	}

	require.NoError(t, cfg.Validate())
}

func TestAPIConfigValidateBasicRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*Config)
		errContains string
	}{
		{
			name: "missing name",
			mutate: func(cfg *Config) {
				cfg.Name = ""
			},
			errContains: "fetcher name cannot be empty",
		},
		{
			name: "missing endpoints",
			mutate: func(cfg *Config) {
				cfg.Endpoints = nil
			},
			errContains: "endpoints cannot be empty",
		},
		{
			name: "empty endpoint URL",
			mutate: func(cfg *Config) {
				cfg.Endpoints = []types.Endpoint{{URL: ""}}
			},
			errContains: "endpoint url cannot be empty",
		},
		{
			name: "negative interval",
			mutate: func(cfg *Config) {
				cfg.Interval = -time.Second
			},
			errContains: "interval cannot be negative",
		},
		{
			name: "negative timeout",
			mutate: func(cfg *Config) {
				cfg.Timeout = -time.Second
			},
			errContains: "timeout cannot be negative",
		},
		{
			name: "negative requests per second",
			mutate: func(cfg *Config) {
				cfg.RequestsPerSecond = -1
			},
			errContains: "requests per second cannot be negative",
		},
		{
			name: "negative batch size",
			mutate: func(cfg *Config) {
				cfg.BatchSize = -1
			},
			errContains: "batch size cannot be negative",
		},
		{
			name: "negative max block height age",
			mutate: func(cfg *Config) {
				cfg.MaxBlockHeightAge = -time.Second
			},
			errContains: "max block height age cannot be negative",
		},
		{
			name: "partial auth missing header",
			mutate: func(cfg *Config) {
				cfg.Endpoints[0].Authentication.APIKey = "secret"
			},
			errContains: "api key header cannot be empty",
		},
		{
			name: "partial auth missing key",
			mutate: func(cfg *Config) {
				cfg.Endpoints[0].Authentication.APIKeyHeader = "X-API-Key"
			},
			errContains: "api key cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Name:      "test",
				Endpoints: []types.Endpoint{{URL: "https://provider.test"}},
			}
			tt.mutate(&cfg)

			require.ErrorContains(t, cfg.Validate(), tt.errContains)
		})
	}
}
