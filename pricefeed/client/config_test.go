package client_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pricefeedclient "ark/pricefeed/client"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*pricefeedclient.Config)
		wantErr string
	}{
		{
			name:   "valid",
			mutate: func(*pricefeedclient.Config) {},
		},
		{
			name: "disabled config still validates runtime fields",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.Enabled = false
				cfg.Interval = 0
			},
			wantErr: "interval must be",
		},
		{
			name: "missing address",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.SidecarAddress = " "
			},
			wantErr: "sidecar address",
		},
		{
			name: "zero client timeout",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.ClientTimeout = 0
			},
			wantErr: "client timeout",
		},
		{
			name: "zero price ttl",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.PriceTTL = 0
			},
			wantErr: "price time to live",
		},
		{
			name: "price ttl above maximum",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.PriceTTL = pricefeedclient.MaxPriceTTL + time.Nanosecond
			},
			wantErr: "price time to live",
		},
		{
			name: "zero interval",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.Interval = 0
			},
			wantErr: "interval must be",
		},
		{
			name: "interval above maximum",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.Interval = pricefeedclient.MaxInterval + time.Nanosecond
			},
			wantErr: "interval must be",
		},
		{
			name: "interval equals price ttl",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.Interval = cfg.PriceTTL
			},
			wantErr: "strictly less",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := pricefeedclient.NewDefaultConfig()
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
				"pricefeed.enabled":         true,
				"pricefeed.sidecar_address": "",
			},
		},
		{
			name: "zero client timeout",
			opts: appOptions{
				"pricefeed.enabled":        true,
				"pricefeed.client_timeout": "0s",
			},
		},
		{
			name: "zero price ttl",
			opts: appOptions{
				"pricefeed.enabled":   true,
				"pricefeed.price_ttl": "0s",
			},
		},
		{
			name: "zero interval",
			opts: appOptions{
				"pricefeed.enabled":  true,
				"pricefeed.interval": "0s",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pricefeedclient.ReadConfigFromAppOpts(tt.opts)
			require.Error(t, err)
		})
	}
}

func TestReadConfigFromAppOpts(t *testing.T) {
	cfg, err := pricefeedclient.ReadConfigFromAppOpts(appOptions{
		"pricefeed.enabled":         true,
		"pricefeed.sidecar_address": "127.0.0.1:9090",
		"pricefeed.client_timeout":  "2s",
		"pricefeed.metrics_enabled": true,
		"pricefeed.price_ttl":       "8s",
		"pricefeed.interval":        "2s",
	})
	require.NoError(t, err)
	require.Equal(t, pricefeedclient.Config{
		Enabled:        true,
		SidecarAddress: "127.0.0.1:9090",
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
