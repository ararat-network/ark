package api_test

import (
	. "noah/oracle/sidecar/providers/base/api"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"noah/oracle/sidecar/providers/types"
)

func TestAPIConfigValidateBasicAllowsPositiveRequiredControls(t *testing.T) {
	cfg := Config{
		Name:      "test",
		Timeout:   time.Second,
		Interval:  time.Second,
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
			name: "zero timeout",
			mutate: func(cfg *Config) {
				cfg.Timeout = 0
			},
			errContains: "timeout must be greater than 0",
		},
		{
			name: "negative timeout",
			mutate: func(cfg *Config) {
				cfg.Timeout = -time.Second
			},
			errContains: "timeout must be greater than 0",
		},
		{
			name: "zero interval",
			mutate: func(cfg *Config) {
				cfg.Interval = 0
			},
			errContains: "interval must be greater than 0",
		},
		{
			name: "negative interval",
			mutate: func(cfg *Config) {
				cfg.Interval = -time.Second
			},
			errContains: "interval must be greater than 0",
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
				Timeout:   time.Second,
				Interval:  time.Second,
				Endpoints: []types.Endpoint{{URL: "https://provider.test"}},
			}
			tt.mutate(&cfg)

			require.ErrorContains(t, cfg.Validate(), tt.errContains)
		})
	}
}

func TestConfigEqual(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{
			name: "equal",
			want: true,
		},
		{
			name: "name differs",
			mutate: func(cfg *Config) {
				cfg.Name = "other"
			},
		},
		{
			name: "timeout differs",
			mutate: func(cfg *Config) {
				cfg.Timeout += time.Second
			},
		},
		{
			name: "interval differs",
			mutate: func(cfg *Config) {
				cfg.Interval += time.Second
			},
		},
		{
			name: "requests per second differs",
			mutate: func(cfg *Config) {
				cfg.RequestsPerSecond++
			},
		},
		{
			name: "endpoints differ",
			mutate: func(cfg *Config) {
				cfg.Endpoints = append(cfg.Endpoints, types.Endpoint{URL: "https://backup.provider.test"})
			},
		},
		{
			name: "batch size differs",
			mutate: func(cfg *Config) {
				cfg.BatchSize++
			},
		},
		{
			name: "max block height age differs",
			mutate: func(cfg *Config) {
				cfg.MaxBlockHeightAge += time.Second
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := Config{
				Name:              "test",
				Timeout:           time.Second,
				Interval:          time.Second,
				RequestsPerSecond: 10,
				Endpoints:         []types.Endpoint{{URL: "https://provider.test"}},
				BatchSize:         100,
				MaxBlockHeightAge: time.Minute,
			}
			b := a
			if tc.mutate != nil {
				tc.mutate(&b)
			}

			require.Equal(t, tc.want, a.Equal(b))
		})
	}
}
