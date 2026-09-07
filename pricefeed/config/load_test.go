package config

import (
	"os"
	"path/filepath"
	"strings"
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
	path := writeConfig(t, validConfigTOML)

	cfg, err := Load(path)

	require.NoError(t, err)
	require.Equal(t, 1500*time.Millisecond, cfg.UpdateInterval)
	require.Equal(t, 90*time.Second, cfg.Providers["frankfurter_api"].MaxPriceAge)
	require.Equal(t, []string{"ausd"}, cfg.FallbackFeeds)
	require.Equal(t, "127.0.0.1:9090", cfg.Client.Address)
	require.Equal(t, []resolver.BootstrapPrice{{
		Pair:       "USD/NOAH",
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
		Pair:       "USD/NOAH",
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
			Pairs: []sidecartypes.Pair{"USD/NOAH"},
		},
		chain.KRWBaseDenom: {
			Name:  "krw-usd-noah",
			Pairs: []sidecartypes.Pair{"KRW/USD", "USD/NOAH"},
		},
		chain.XDRBaseDenom: {
			Name:  "xdr-usd-noah",
			Pairs: []sidecartypes.Pair{"XDR/USD", "USD/NOAH"},
		},
		chain.CNYBaseDenom: {
			Name:  "cny-usd-noah",
			Pairs: []sidecartypes.Pair{"CNY/USD", "USD/NOAH"},
		},
		chain.JPYBaseDenom: {
			Name:  "jpy-usd-noah",
			Pairs: []sidecartypes.Pair{"JPY/USD", "USD/NOAH"},
		},
		chain.EURBaseDenom: {
			Name:  "eur-usd-noah",
			Pairs: []sidecartypes.Pair{"EUR/USD", "USD/NOAH"},
		},
		chain.GBPBaseDenom: {
			Name:  "gbp-usd-noah",
			Pairs: []sidecartypes.Pair{"GBP/USD", "USD/NOAH"},
		},
		chain.CADBaseDenom: {
			Name:  "cad-usd-noah",
			Pairs: []sidecartypes.Pair{"CAD/USD", "USD/NOAH"},
		},
		chain.AUDBaseDenom: {
			Name:  "aud-usd-noah",
			Pairs: []sidecartypes.Pair{"AUD/USD", "USD/NOAH"},
		},
		chain.SGDBaseDenom: {
			Name:  "sgd-usd-noah",
			Pairs: []sidecartypes.Pair{"SGD/USD", "USD/NOAH"},
		},
		chain.MXNBaseDenom: {
			Name:  "mxn-usd-noah",
			Pairs: []sidecartypes.Pair{"MXN/USD", "USD/NOAH"},
		},
	}

	for _, denom := range cfg.FallbackFeeds {
		routes, ok := cfg.Resolver.Routes[denom]
		require.True(t, ok, "missing default resolver route for %s", denom)
		require.Equal(t, []resolver.Route{expectedRoutes[denom]}, routes)
	}
}

// TestDefaultFrankfurterMarketsSupplyFiatRouteLegs pins where venue
// orientation stops: every default fiat route opens with a UNIT/USD leg,
// Frankfurter quotes the dollar as base, so the leg is served by the
// USD/UNIT market and the resolver normalises each sample.
func TestDefaultFrankfurterMarketsSupplyFiatRouteLegs(t *testing.T) {
	cfg := Default()
	markets := cfg.Providers[frankfurter.Name].Markets

	legs := 0
	for denom, routes := range cfg.Resolver.Routes {
		if denom == chain.USDBaseDenom {
			continue
		}
		require.Len(t, routes, 1, "default route for %s", denom)
		leg := routes[0].Pairs[0]
		require.Equal(t, "USD", leg.Quote(), "default route for %s must open on a UNIT/USD leg", denom)

		market := leg.Inverse()
		ticker, ok := markets.PairToTicker(market)
		require.True(t, ok, "missing default Frankfurter market for %s", market)
		require.Equal(t, providertypes.Ticker(market.String()), ticker)
		legs++
	}
	require.Len(t, markets, legs)
}

func TestLoadRejectsEmptyPath(t *testing.T) {
	_, err := Load(" ")

	require.ErrorContains(t, err, "config path cannot be empty")
}

func TestLoadValidatesDecodedConfig(t *testing.T) {
	path := writeConfig(t, `update_interval = "0s"`)

	_, err := Load(path)

	require.ErrorContains(t, err, "update interval must be greater than 0")
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "oracle.toml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

const validConfigTOML = `update_interval = "1500ms"
fallback_feeds = ["ausd"]

[client]
address = "127.0.0.1:9090"
timeout = "2s"
interval = "5s"

[providers.frankfurter_api]
name = "frankfurter_api"
transport_type = "api"
max_price_age = "90s"
markets = [{ pair = "NOAH/USD", symbol = "NOAHUSD" }]

[providers.frankfurter_api.api]
name = "frankfurter_api"
timeout = "3s"
interval = "1m"
batch_size = 1

[[providers.frankfurter_api.api.endpoints]]
url = "https://api.frankfurter.dev/v2/rates"

[[resolver.bootstrap_prices]]
pair = "USD/NOAH"
price = "0.25"
valid_until = "2030-01-01T00:00:00Z"
`

func TestLoadRejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{name: "top level", contents: validConfigTOML + "\nupdate_intreval = \"1s\"\n"},
		{name: "nested table", contents: strings.Replace(validConfigTOML, "[client]", "[client]\ntimeuot = \"2s\"", 1)},
		{name: "array of tables", contents: validConfigTOML + "\n[[resolver.bootstrap_prices]]\npair = \"USD/NOAH\"\nprice = \"1\"\nvalid_untl = \"2030-01-01T00:00:00Z\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.contents))

			require.ErrorContains(t, err, "invalid keys")
		})
	}
}

func TestLoadIgnoresExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pricefeed")
	require.NoError(t, os.WriteFile(path, []byte(validConfigTOML), 0o600))

	cfg, err := Load(path)

	require.NoError(t, err)
	require.Equal(t, 1500*time.Millisecond, cfg.UpdateInterval)
}
