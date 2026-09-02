package currencybeacon

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the CurrencyBeacon provider.
	Name = "currencybeacon_api"

	// URL is the CurrencyBeacon latest-rates endpoint.
	URL = "https://api.currencybeacon.com/v1/latest"

	// DefaultMaxPriceAge is the default freshness window for CurrencyBeacon
	// prices. It covers one missed poll of the default interval.
	DefaultMaxPriceAge = 25 * time.Minute

	// PlaceholderAPIKey must be replaced with a real key. The Bearer scheme
	// prefix stays: the fetcher sends the value verbatim in the header.
	PlaceholderAPIKey = "Bearer YOUR_API_KEY"
)

// DefaultAPIConfig is the default configuration for the CurrencyBeacon API.
// The interval stays inside the free plan's 5,000 requests per month.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          15 * time.Minute,
	RequestsPerSecond: 0,
	Endpoints: []types.Endpoint{{
		URL: URL,
		Authentication: types.Authentication{
			APIKey:       PlaceholderAPIKey,
			APIKeyHeader: "Authorization",
		},
	}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in CurrencyBeacon fiat pair mappings.
//
// USD/XDR is absent by design: CurrencyBeacon's supported list carries no XDR,
// so the reference prices through the other two fiat providers.
var DefaultMarkets = types.Markets{
	{Pair: "USD/KRW", Symbol: "USD/KRW"},
	{Pair: "USD/CNY", Symbol: "USD/CNY"},
	{Pair: "USD/JPY", Symbol: "USD/JPY"},
	{Pair: "USD/EUR", Symbol: "USD/EUR"},
	{Pair: "USD/GBP", Symbol: "USD/GBP"},
	{Pair: "USD/CAD", Symbol: "USD/CAD"},
	{Pair: "USD/AUD", Symbol: "USD/AUD"},
	{Pair: "USD/SGD", Symbol: "USD/SGD"},
	{Pair: "USD/MXN", Symbol: "USD/MXN"},
}
