package oracle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"noah/oracle/providers"
	providertypes "noah/oracle/providers/types"
)

func TestConfigValidateRejectsInvalidFields(t *testing.T) {
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}

	testCases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "zero update interval",
			mutate: func(cfg *Config) {
				cfg.UpdateInterval = 0
			},
			wantErr: "oracle update interval must be greater than 0",
		},
		{
			name: "zero max price age",
			mutate: func(cfg *Config) {
				cfg.MaxPriceAge = 0
			},
			wantErr: "oracle max price age must be greater than 0",
		},
		{
			name: "invalid provider config",
			mutate: func(cfg *Config) {
				providerCfg := testUnknownAPIProviderConfig("binance", markets)
				providerCfg.API.Endpoints = nil
				cfg.Providers = map[string]providers.Config{
					"binance": providerCfg,
				}
			},
			wantErr: "provider is not formatted correctly",
		},
		{
			name: "empty host",
			mutate: func(cfg *Config) {
				cfg.Host = ""
			},
			wantErr: "oracle host cannot be empty",
		},
		{
			name: "empty port",
			mutate: func(cfg *Config) {
				cfg.Port = ""
			},
			wantErr: "oracle port cannot be empty",
		},
		{
			name: "empty denoms",
			mutate: func(cfg *Config) {
				cfg.Denoms = nil
			},
			wantErr: "oracle denoms cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testOracleConfig([]string{"uusd"}, map[string]providers.Config{})
			tc.mutate(&cfg)

			err := cfg.Validate()

			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestConfigValidateRejectsProviderMapKeyThatDiffersFromProviderName(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testUnknownAPIProviderConfig("binance", markets)
	cfg := testOracleConfig(denoms, map[string]providers.Config{
		"binance-main": providerCfg,
	})

	err := cfg.Validate()

	require.ErrorContains(t, err, "provider map key")
}

func TestConfigValidateAcceptsProviderMapKeyThatMatchesProviderName(t *testing.T) {
	denoms := []string{"uusd"}
	markets := providertypes.Markets{{Denom: "uusd", Symbol: "USDTUSD"}}
	providerCfg := testUnknownAPIProviderConfig("binance", markets)
	cfg := testOracleConfig(denoms, map[string]providers.Config{
		"binance": providerCfg,
	})

	require.NoError(t, cfg.Validate())
}

func TestReadOracleConfigFromFile(t *testing.T) {
	t.Cleanup(viper.Reset)

	path := writeOracleConfigFile(t, `{
		"updateInterval": "1s",
		"maxPriceAge": "1m",
		"providers": {
			"unknown": {
				"name": "unknown",
				"type": "api",
				"markets": [
					{"denom": "uusd", "symbol": "USDTUSD"}
				],
				"api": {
					"name": "unknown",
					"interval": "1s",
					"endpoints": [
						{"url": "https://example.invalid/prices"}
					]
				}
			}
		},
		"host": "127.0.0.1",
		"port": "0",
		"denoms": ["uusd"]
	}`)

	cfg, err := ReadOracleConfigFromFile(path)

	require.NoError(t, err)
	require.Equal(t, time.Second, cfg.UpdateInterval)
	require.Equal(t, time.Minute, cfg.MaxPriceAge)
	require.Equal(t, "127.0.0.1", cfg.Host)
	require.Equal(t, "0", cfg.Port)
	require.Equal(t, []string{"uusd"}, cfg.Denoms)
	require.Contains(t, cfg.Providers, "unknown")
}

func TestReadOracleConfigFromFileReturnsReadError(t *testing.T) {
	t.Cleanup(viper.Reset)

	_, err := ReadOracleConfigFromFile(filepath.Join(t.TempDir(), "missing.json"))

	require.Error(t, err)
}

func TestReadOracleConfigFromFileReturnsParseError(t *testing.T) {
	t.Cleanup(viper.Reset)
	path := writeOracleConfigFile(t, `{`)

	_, err := ReadOracleConfigFromFile(path)

	require.Error(t, err)
}

func TestReadOracleConfigFromFileReturnsValidationError(t *testing.T) {
	t.Cleanup(viper.Reset)
	path := writeOracleConfigFile(t, `{
		"updateInterval": "1s",
		"maxPriceAge": "1m",
		"providers": {},
		"host": "127.0.0.1",
		"port": "0",
		"denoms": []
	}`)

	_, err := ReadOracleConfigFromFile(path)

	require.ErrorContains(t, err, "oracle denoms cannot be empty")
}

func writeOracleConfigFile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "oracle.json")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}
