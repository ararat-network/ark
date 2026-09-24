package coingecko

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: The endpoint is documented at
// https://docs.coingecko.com/reference/simple-price. The public endpoint is
// rate limited and needs no key; the pro endpoint needs one.

const (
	// Name is the name of the CoinGecko API provider.
	Name = "coingecko_api"

	// URL is the public CoinGecko simple price endpoint. It needs no API key
	// but is rate limited.
	URL = "https://api.coingecko.com/api/v3/simple/price"

	// ProURL is the CoinGecko pro simple price endpoint. It needs an API key
	// sent in ProAPIKeyHeader.
	ProURL = "https://pro-api.coingecko.com/api/v3/simple/price"

	// ProAPIKeyHeader carries a pro API key.
	ProAPIKeyHeader = "x-cg-pro-api-key"

	// DemoAPIKeyHeader carries a demo API key on the public endpoint.
	DemoAPIKeyHeader = "x-cg-demo-api-key"

	// Precision is the number of decimal places requested for every price.
	Precision = "18"

	// TickerSeparator separates the coin id from the quote currency in a
	// provider symbol, for example tether/usd.
	TickerSeparator = "/"
)

// DefaultAPIConfig is the default configuration for the CoinGecko API. The
// public endpoint's rate limit is low, so polling is slow.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          20 * time.Second,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in CoinGecko API pair mappings. The symbol
// is the CoinGecko coin id, a separator, and the quote currency.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "tether/usd"},
}
