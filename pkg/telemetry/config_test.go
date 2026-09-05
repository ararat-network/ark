package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrometheusConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*PrometheusConfig)
		errorSubstr string
	}{
		{
			name:   "defaults",
			mutate: func(*PrometheusConfig) {},
		},
		{
			name:   "any interface",
			mutate: func(c *PrometheusConfig) { c.Address = "0.0.0.0:9464" },
		},
		{
			name:   "port only",
			mutate: func(c *PrometheusConfig) { c.Address = ":9464" },
		},
		{
			name:        "empty address",
			mutate:      func(c *PrometheusConfig) { c.Address = "" },
			errorSubstr: "address must be host:port",
		},
		{
			name:        "missing port",
			mutate:      func(c *PrometheusConfig) { c.Address = "localhost" },
			errorSubstr: "address must be host:port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultPrometheusConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.errorSubstr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.errorSubstr)
		})
	}
}
