package sidecar

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	frankfurterapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	baseapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
)

// testCustomProviderRuntimeConfig renames the fixture's frankfurter provider so
// only a registry carrying the custom name can build it.
func testCustomProviderRuntimeConfig(name string) Config {
	cfg := testInternalRuntimeConfig()
	providerCfg := cfg.Providers[frankfurterapi.Name]
	providerCfg.Name = name
	providerCfg.API.Name = name
	delete(cfg.Providers, frankfurterapi.Name)
	cfg.Providers[name] = providerCfg

	return Config{Runtime: cfg}
}

func TestNewServiceWithRegistry(t *testing.T) {
	t.Run("default registry rejects custom provider", func(t *testing.T) {
		_, err := NewService(testCustomProviderRuntimeConfig("custom_api"), nil)

		require.ErrorContains(t, err, "unrecognised provider name: custom_api")
	})

	t.Run("custom registry builds custom provider", func(t *testing.T) {
		registry := providers.NewRegistry()
		require.NoError(t, registry.RegisterAPI("custom_api", func(providers.Config, log.Logger) (baseapi.DataHandler, error) {
			return frankfurterapi.NewHandler(), nil
		}))

		oracle, err := NewService(testCustomProviderRuntimeConfig("custom_api"), nil, WithRegistry(registry))

		require.NoError(t, err)
		require.NotNil(t, oracle)
	})
}
