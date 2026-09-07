package client_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	pricefeedclient "github.com/ararat-network/ark/pricefeed/client"
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
			name:    "blank address",
			mutate:  func(cfg *pricefeedclient.Config) { cfg.SidecarAddress = " " },
			wantErr: "sidecar address must not be empty",
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
			wantErr: "price_ttl",
		},
		{
			name: "price ttl above maximum",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.PriceTTL = pricefeedclient.MaxPriceTTL + time.Nanosecond
			},
			wantErr: "price_ttl",
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

func TestReadConfigFromAppOpts(t *testing.T) {
	cfg, err := pricefeedclient.ReadConfigFromAppOpts(appOptions{
		"pricefeed.enabled":         true,
		"pricefeed.sidecar_address": "127.0.0.1:9090",
		"pricefeed.client_timeout":  "2s",
		"pricefeed.price_ttl":       "8s",
		"pricefeed.interval":        "2s",
	})
	require.NoError(t, err)
	require.Equal(t, pricefeedclient.Config{
		Enabled:        true,
		SidecarAddress: "127.0.0.1:9090",
		ClientTimeout:  2 * time.Second,
		PriceTTL:       8 * time.Second,
		Interval:       2 * time.Second,
	}, cfg)
}

func TestReadConfigFromAppOptsAbsentKeysDefault(t *testing.T) {
	cfg, err := pricefeedclient.ReadConfigFromAppOpts(appOptions{})
	require.NoError(t, err)
	require.Equal(t, pricefeedclient.NewDefaultConfig(), cfg)
}

// The reader decodes and nothing more. What it refuses is a value of the
// wrong type, and it names every such key at once.
func TestReadConfigFromAppOptsRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name         string
		opts         appOptions
		errorSubstrs []string
	}{
		{
			name:         "enabled not a bool",
			opts:         appOptions{"pricefeed.enabled": "sometimes"},
			errorSubstrs: []string{"pricefeed.enabled"},
		},
		{
			name:         "interval not a duration",
			opts:         appOptions{"pricefeed.interval": "soon"},
			errorSubstrs: []string{"pricefeed.interval"},
		},
		{
			name: "every malformed key reported",
			opts: appOptions{
				"pricefeed.client_timeout": "later",
				"pricefeed.price_ttl":      "never",
			},
			errorSubstrs: []string{"pricefeed.client_timeout", "pricefeed.price_ttl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pricefeedclient.ReadConfigFromAppOpts(tt.opts)
			require.Error(t, err)
			for _, substr := range tt.errorSubstrs {
				require.ErrorContains(t, err, substr)
			}
		})
	}
}

// Out-of-range values pass the reader; NewClient is the backstop that
// refuses them for the commands that build the app without starting it.
func TestReadConfigFromAppOptsDoesNotValidate(t *testing.T) {
	cfg, err := pricefeedclient.ReadConfigFromAppOpts(appOptions{"pricefeed.interval": "0s"})
	require.NoError(t, err)

	_, err = pricefeedclient.NewClient(log.NewNopLogger(), cfg)
	require.ErrorContains(t, err, "interval must be")
}

type appOptions map[string]any

func (o appOptions) Get(key string) any {
	return o[key]
}
