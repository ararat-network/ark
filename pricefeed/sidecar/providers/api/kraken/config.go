package kraken

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: The endpoint is documented at
// https://docs.kraken.com/api/docs/rest-api/get-ticker-information and needs
// no API key.

const (
	// Name is the name of the Kraken API provider.
	Name = "kraken_api"

	// URL is the Kraken public ticker endpoint. The requested pairs go in the
	// pair query parameter.
	URL = "https://api.kraken.com/0/public/Ticker"

	// Separator separates pairs in the query parameter.
	Separator = ","
)

// DefaultAPIConfig is the default configuration for the Kraken API.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          600 * time.Millisecond,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in Kraken API pair mappings. Responses are
// keyed by Kraken's full pair name, so the symbol is USDTZUSD rather than the
// USDTUSD alternate name; the request accepts either.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDTZUSD"},
}
