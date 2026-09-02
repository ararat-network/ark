package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
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

func TestReadPrometheusConfig(t *testing.T) {
	tests := []struct {
		name        string
		opts        simtestutil.AppOptionsMap
		expected    PrometheusConfig
		errorSubstr string
	}{
		{
			name:     "absent keys fall back to defaults",
			opts:     simtestutil.AppOptionsMap{},
			expected: DefaultPrometheusConfig(),
		},
		{
			name: "enabled with address",
			opts: simtestutil.AppOptionsMap{
				flagPrometheusEnabled: "true",
				flagPrometheusAddress: "0.0.0.0:9465",
			},
			expected: PrometheusConfig{Enabled: true, Address: "0.0.0.0:9465"},
		},
		{
			name:        "malformed enabled",
			opts:        simtestutil.AppOptionsMap{flagPrometheusEnabled: "sometimes"},
			errorSubstr: "prometheus enabled must be a boolean",
		},
		{
			name:        "malformed address",
			opts:        simtestutil.AppOptionsMap{flagPrometheusAddress: "localhost"},
			errorSubstr: "address must be host:port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ReadPrometheusConfig(tt.opts)
			if tt.errorSubstr != "" {
				require.ErrorContains(t, err, tt.errorSubstr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, cfg)
		})
	}
}
