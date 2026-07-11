package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ark/oracle/sidecar/providers/api/frankfurter"
	providertypes "ark/oracle/sidecar/providers/types"
	"ark/oracle/sidecar/resolver"
	sidecartypes "ark/oracle/sidecar/types"
	"ark/pkg/chain"
)

func TestLoadDecodesDurationStrings(t *testing.T) {
	path := writeConfig(t, validConfigJSON)

	cfg, err := Load(path)

	require.NoError(t, err)
	require.Equal(t, 1500*time.Millisecond, cfg.UpdateInterval)
	require.Equal(t, 90*time.Second, cfg.Providers["frankfurter_api"].MaxPriceAge)
	require.Equal(t, []string{"uusd"}, cfg.FallbackDenoms)
	require.Equal(t, []resolver.BootstrapPrice{{
		Pair:       "NOAH/USD",
		Price:      "0.25",
		ValidUntil: "2030-01-01T00:00:00Z",
	}}, cfg.Resolver.BootstrapPrices)
}

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()

	require.NoError(t, cfg.Validate())
	require.NotEmpty(t, cfg.Providers)
	require.NotEmpty(t, cfg.FallbackDenoms)
	require.Equal(t, 90*time.Second, cfg.Providers["frankfurter_api"].MaxPriceAge)
	require.NotNil(t, cfg.Resolver.BootstrapPrices)
	require.Equal(t, []resolver.BootstrapPrice{{
		Pair:       "NOAH/USD",
		Price:      "1",
		ValidUntil: "2027-10-29T00:00:00Z",
	}}, cfg.Resolver.BootstrapPrices)
}

func TestDefaultResolverRoutesFiatDenomsThroughUSD(t *testing.T) {
	cfg := Default()
	require.NotContains(t, cfg.Resolver.Routes, chain.MicroNoahDenom)

	expectedRoutes := map[string]resolver.Route{
		chain.MicroUSDDenom: {
			Name:  "direct",
			Pairs: []sidecartypes.Pair{"NOAH/USD"},
		},
		chain.MicroKRWDenom: {
			Name:  "noah-usd-krw",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/KRW"},
		},
		chain.MicroSDRDenom: {
			Name:  "noah-usd-sdr",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/SDR"},
		},
		chain.MicroCNYDenom: {
			Name:  "noah-usd-cny",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/CNY"},
		},
		chain.MicroJPYDenom: {
			Name:  "noah-usd-jpy",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/JPY"},
		},
		chain.MicroEURDenom: {
			Name:  "noah-usd-eur",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/EUR"},
		},
		chain.MicroGBPDenom: {
			Name:  "noah-usd-gbp",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/GBP"},
		},
		chain.MicroMNTDenom: {
			Name:  "noah-usd-mnt",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/MNT"},
		},
	}

	for _, denom := range cfg.FallbackDenoms {
		routes, ok := cfg.Resolver.Routes[denom]
		require.True(t, ok, "missing default resolver route for %s", denom)
		require.Equal(t, []resolver.Route{expectedRoutes[denom]}, routes)
	}
}

func TestDefaultFrankfurterMarketsSupplyFiatRouteLegs(t *testing.T) {
	markets := Default().Providers[frankfurter.Name].Markets
	expected := map[sidecartypes.Pair]providertypes.Ticker{
		"USD/KRW": "USD/KRW",
		"USD/SDR": "USD/XDR",
		"USD/CNY": "USD/CNY",
		"USD/JPY": "USD/JPY",
		"USD/EUR": "USD/EUR",
		"USD/GBP": "USD/GBP",
		"USD/MNT": "USD/MNT",
	}

	require.Len(t, markets, len(expected))
	for pair, wantTicker := range expected {
		ticker, ok := markets.PairToTicker(pair)
		require.True(t, ok, "missing default Frankfurter market for %s", pair)
		require.Equal(t, wantTicker, ticker)
	}
}

func TestLoadRejectsEmptyPath(t *testing.T) {
	_, err := Load(" ")

	require.ErrorContains(t, err, "config path cannot be empty")
}

func TestLoadValidatesDecodedConfig(t *testing.T) {
	path := writeConfig(t, `{"updateInterval":"0s"}`)

	_, err := Load(path)

	require.ErrorContains(t, err, "update interval must be greater than 0")
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "oracle.json")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

const validConfigJSON = `{
  "updateInterval": "1500ms",
  "providers": {
    "frankfurter_api": {
      "name": "frankfurter_api",
      "transportType": "api",
      "maxPriceAge": "90s",
      "markets": [{"pair": "NOAH/USD", "symbol": "NOAHUSD"}],
      "api": {
        "name": "frankfurter_api",
        "timeout": "3s",
        "interval": "1m",
        "endpoints": [{"url": "https://api.frankfurter.dev/v2/rates"}],
        "batchSize": 1
      }
    }
  },
  "resolver": {
    "bootstrapPrices": [{
      "pair": "NOAH/USD",
      "price": "0.25",
      "validUntil": "2030-01-01T00:00:00Z"
    }]
  },
  "client": {
    "address": "127.0.0.1:9090",
    "timeout": "2s",
    "interval": "5s"
  },
  "fallbackDenoms": ["uusd"]
}`
