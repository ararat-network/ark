package client_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	oracleclient "ark/oracle/client"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*oracleclient.Config)
		wantErr string
	}{
		{
			name:   "valid",
			mutate: func(*oracleclient.Config) {},
		},
		{
			name: "disabled config still validates runtime fields",
			mutate: func(cfg *oracleclient.Config) {
				cfg.Enabled = false
				cfg.Interval = 0
			},
			wantErr: "oracle interval",
		},
		{
			name: "missing address",
			mutate: func(cfg *oracleclient.Config) {
				cfg.OracleAddress = " "
			},
			wantErr: "oracle address",
		},
		{
			name: "zero client timeout",
			mutate: func(cfg *oracleclient.Config) {
				cfg.ClientTimeout = 0
			},
			wantErr: "oracle client timeout",
		},
		{
			name: "zero price ttl",
			mutate: func(cfg *oracleclient.Config) {
				cfg.PriceTTL = 0
			},
			wantErr: "oracle price time to live",
		},
		{
			name: "price ttl above maximum",
			mutate: func(cfg *oracleclient.Config) {
				cfg.PriceTTL = oracleclient.MaxPriceTTL + time.Nanosecond
			},
			wantErr: "oracle price time to live",
		},
		{
			name: "zero interval",
			mutate: func(cfg *oracleclient.Config) {
				cfg.Interval = 0
			},
			wantErr: "oracle interval",
		},
		{
			name: "interval above maximum",
			mutate: func(cfg *oracleclient.Config) {
				cfg.Interval = oracleclient.MaxInterval + time.Nanosecond
			},
			wantErr: "oracle interval",
		},
		{
			name: "interval equals price ttl",
			mutate: func(cfg *oracleclient.Config) {
				cfg.Interval = cfg.PriceTTL
			},
			wantErr: "strictly less",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := oracleclient.NewDefaultConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestReadConfigFromAppOptsRejectsExplicitInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		opts appOptions
	}{
		{
			name: "empty address",
			opts: appOptions{
				"oracle.enabled":        true,
				"oracle.oracle_address": "",
			},
		},
		{
			name: "zero client timeout",
			opts: appOptions{
				"oracle.enabled":        true,
				"oracle.client_timeout": "0s",
			},
		},
		{
			name: "zero price ttl",
			opts: appOptions{
				"oracle.enabled":   true,
				"oracle.price_ttl": "0s",
			},
		},
		{
			name: "zero interval",
			opts: appOptions{
				"oracle.enabled":  true,
				"oracle.interval": "0s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := oracleclient.ReadConfigFromAppOpts(tt.opts)
			require.Error(t, err)
		})
	}
}

func TestReadConfigFromAppOpts(t *testing.T) {
	cfg, err := oracleclient.ReadConfigFromAppOpts(appOptions{
		"oracle.enabled":         true,
		"oracle.oracle_address":  "127.0.0.1:9090",
		"oracle.client_timeout":  "2s",
		"oracle.metrics_enabled": true,
		"oracle.price_ttl":       "8s",
		"oracle.interval":        "2s",
	})
	require.NoError(t, err)
	require.Equal(t, oracleclient.Config{
		Enabled:        true,
		OracleAddress:  "127.0.0.1:9090",
		ClientTimeout:  2 * time.Second,
		MetricsEnabled: true,
		PriceTTL:       8 * time.Second,
		Interval:       2 * time.Second,
	}, cfg)
}

type appOptions map[string]any

func (o appOptions) Get(key string) any {
	return o[key]
}
