package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func TestLoadDecodesDurationStrings(t *testing.T) {
	path := writeConfig(t, validConfigJSON)

	cfg, err := Load(path)

	require.NoError(t, err)
	require.Equal(t, 1500*time.Millisecond, cfg.UpdateInterval)
	require.Equal(t, 90*time.Second, cfg.Providers["frankfurter_api"].MaxPriceAge)
	require.Equal(t, []string{"ausd"}, cfg.FallbackFeeds)
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
	require.NotEmpty(t, cfg.FallbackFeeds)
	require.Equal(t, DefaultClientInterval, cfg.Client.Interval)
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
	require.NotContains(t, cfg.Resolver.Routes, chain.NoahBaseDenom)

	expectedRoutes := map[string]resolver.Route{
		chain.USDBaseDenom: {
			Name:  "direct",
			Pairs: []sidecartypes.Pair{"NOAH/USD"},
		},
		chain.KRWBaseDenom: {
			Name:  "noah-usd-krw",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/KRW"},
		},
		chain.XDRBaseDenom: {
			Name:  "noah-usd-xdr",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/XDR"},
		},
		chain.CNYBaseDenom: {
			Name:  "noah-usd-cny",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/CNY"},
		},
		chain.JPYBaseDenom: {
			Name:  "noah-usd-jpy",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/JPY"},
		},
		chain.EURBaseDenom: {
			Name:  "noah-usd-eur",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/EUR"},
		},
		chain.GBPBaseDenom: {
			Name:  "noah-usd-gbp",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/GBP"},
		},
		chain.CADBaseDenom: {
			Name:  "noah-usd-cad",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/CAD"},
		},
		chain.AUDBaseDenom: {
			Name:  "noah-usd-aud",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/AUD"},
		},
		chain.SGDBaseDenom: {
			Name:  "noah-usd-sgd",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/SGD"},
		},
		chain.MXNBaseDenom: {
			Name:  "noah-usd-mxn",
			Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/MXN"},
		},
	}

	for _, denom := range cfg.FallbackFeeds {
		routes, ok := cfg.Resolver.Routes[denom]
		require.True(t, ok, "missing default resolver route for %s", denom)
		require.Equal(t, []resolver.Route{expectedRoutes[denom]}, routes)
	}
}

func TestDefaultFrankfurterMarketsSupplyFiatRouteLegs(t *testing.T) {
	markets := Default().Providers[frankfurter.Name].Markets
	expected := map[sidecartypes.Pair]providertypes.Ticker{
		"USD/KRW": "USD/KRW",
		"USD/XDR": "USD/XDR",
		"USD/CNY": "USD/CNY",
		"USD/JPY": "USD/JPY",
		"USD/EUR": "USD/EUR",
		"USD/GBP": "USD/GBP",
		"USD/CAD": "USD/CAD",
		"USD/AUD": "USD/AUD",
		"USD/SGD": "USD/SGD",
		"USD/MXN": "USD/MXN",
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
  "fallbackFeeds": ["ausd"]
}`
