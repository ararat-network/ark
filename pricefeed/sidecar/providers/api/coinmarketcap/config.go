package coinmarketcap

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: The endpoint is documented at
// https://coinmarketcap.com/api/documentation/v1/#operation/getV2CryptocurrencyQuotesLatest
// and needs an API key.

const (
	// Name is the name of the CoinMarketCap API provider.
	Name = "coinmarketcap_api"

	// URL is the CoinMarketCap latest quotes endpoint. The requested ids go in
	// the id query parameter.
	URL = "https://pro-api.coinmarketcap.com/v2/cryptocurrency/quotes/latest"

	// APIKeyHeader carries the API key.
	APIKeyHeader = "X-CMC_PRO_API_KEY"

	// PlaceholderAPIKey must be replaced with a real key.
	PlaceholderAPIKey = "YOUR_API_KEY"

	// QuoteCurrency is the quote every price is read in. The endpoint quotes
	// in USD unless a convert parameter says otherwise.
	QuoteCurrency = "USD"
)

// DefaultAPIConfig is the default configuration for the CoinMarketCap API.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          2000 * time.Millisecond,
	RequestsPerSecond: 0,
	Endpoints: []types.Endpoint{{
		URL: URL,
		Authentication: types.Authentication{
			APIKey:       PlaceholderAPIKey,
			APIKeyHeader: APIKeyHeader,
		},
	}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in CoinMarketCap API pair mappings. The
// symbol is the CoinMarketCap id of the asset priced in USD.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "825"},
}
