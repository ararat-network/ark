package client_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/tlsconfig"
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
			name: "no addresses",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.SidecarAddresses = nil
			},
			wantErr: "at least one address",
		},
		{
			name: "blank address",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.SidecarAddresses = []string{"127.0.0.1:8080", " "}
			},
			wantErr: "sidecar_addresses[1] must not be empty",
		},
		{
			name: "duplicate address",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.SidecarAddresses = []string{"127.0.0.1:8080", "127.0.0.1:8080"}
			},
			wantErr: "sidecar_addresses[1] repeats",
		},
		{
			name: "addresses at maximum",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.SidecarAddresses = sidecarAddresses(pricefeedclient.MaxSidecarAddresses)
				cfg.TLS.Mode = tlsconfig.Plaintext
			},
		},
		{
			name: "addresses above maximum",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.SidecarAddresses = sidecarAddresses(pricefeedclient.MaxSidecarAddresses + 1)
			},
			wantErr: "at most",
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
		{
			name: "tls ca alone",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.TLS.Mode = tlsconfig.TLS
				cfg.TLS.CAFile = "ca.pem"
			},
		},
		{
			name: "tls client certificate",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.TLS.Mode = tlsconfig.TLS
				cfg.TLS.CAFile = "ca.pem"
				cfg.TLS.CertFile = "node.pem"
				cfg.TLS.KeyFile = "node.key"
				cfg.TLS.ServerName = "sidecar"
			},
		},
		{
			name: "tls cert without key",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.TLS.Mode = tlsconfig.TLS
				cfg.TLS.CAFile = "ca.pem"
				cfg.TLS.CertFile = "node.pem"
			},
			wantErr: "set together",
		},
		{
			name: "tls cert without ca",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.TLS.CertFile = "node.pem"
				cfg.TLS.KeyFile = "node.key"
			},
			wantErr: "require mode tls",
		},
		{
			name: "tls server name without ca",
			mutate: func(cfg *pricefeedclient.Config) {
				cfg.TLS.ServerName = "sidecar"
			},
			wantErr: "require mode tls",
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
		"pricefeed.enabled": true,
		// A TOML array arrives from viper as []any.
		"pricefeed.sidecar_addresses": []any{"127.0.0.1:9090", "10.0.0.2:9090"},
		"pricefeed.client_timeout":    "2s",
		"pricefeed.price_ttl":         "8s",
		"pricefeed.interval":          "2s",
		"pricefeed.tls.mode":          tlsconfig.TLS,
		"pricefeed.tls.ca_file":       "ca.pem",
		"pricefeed.tls.cert_file":     "node.pem",
		"pricefeed.tls.key_file":      "node.key",
		"pricefeed.tls.server_name":   "sidecar",
	})
	require.NoError(t, err)
	require.Equal(t, pricefeedclient.Config{
		Enabled:          true,
		SidecarAddresses: []string{"127.0.0.1:9090", "10.0.0.2:9090"},
		ClientTimeout:    2 * time.Second,
		PriceTTL:         8 * time.Second,
		Interval:         2 * time.Second,
		TLS: tlsconfig.Client{
			Mode:       tlsconfig.TLS,
			CAFile:     "ca.pem",
			CertFile:   "node.pem",
			KeyFile:    "node.key",
			ServerName: "sidecar",
		},
	}, cfg)
}

// An environment override arrives as one string and splits on commas, as
// viper's decoder splits it for the start command.
func TestReadConfigFromAppOptsSplitsAddressString(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "two addresses",
			value: "127.0.0.1:9090,10.0.0.2:9090",
			want:  []string{"127.0.0.1:9090", "10.0.0.2:9090"},
		},
		{
			name:  "empty",
			value: "",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := pricefeedclient.ReadConfigFromAppOpts(appOptions{
				"pricefeed.sidecar_addresses": tt.value,
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.SidecarAddresses)
		})
	}
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

func sidecarAddresses(n int) []string {
	addresses := make([]string, n)
	for i := range addresses {
		addresses[i] = fmt.Sprintf("sidecar-%d:8080", i)
	}
	return addresses
}

type appOptions map[string]any

func (o appOptions) Get(key string) any {
	return o[key]
}

// The singular key is refused, not ignored: viper reports nothing for a key
// it does not know, and a node that kept it would poll the default address.
func TestReadConfigFromAppOptsRefusesRenamedKey(t *testing.T) {
	_, err := pricefeedclient.ReadConfigFromAppOpts(appOptions{
		"pricefeed.sidecar_address": "10.0.0.2:8080",
	})

	require.ErrorContains(t, err, "pricefeed.sidecar_address was renamed to pricefeed.sidecar_addresses")
}
