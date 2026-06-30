package oracle

import (
	"testing"

	"github.com/stretchr/testify/require"

	"noah/oracle/providers"
	providertypes "noah/oracle/providers/types"
)

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
