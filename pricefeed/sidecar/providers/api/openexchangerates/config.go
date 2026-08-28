package openexchangerates

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	// Name is the name of the Open Exchange Rates provider.
	Name = "openexchangerates_api"

	// URL is the Open Exchange Rates latest-rates endpoint.
	URL = "https://openexchangerates.org/api/latest.json"

	// DefaultMaxPriceAge is the default freshness window for Open Exchange
	// Rates prices. It covers one missed poll of the default hourly interval.
	DefaultMaxPriceAge = 90 * time.Minute

	// PlaceholderAPIKey must be replaced with a real app ID. The Token scheme
	// prefix stays: the fetcher sends the value verbatim in the header.
	PlaceholderAPIKey = "Token YOUR_APP_ID"
)

// DefaultAPIConfig is the default configuration for the Open Exchange Rates
// API. The hourly interval stays inside the free plan's 1,000 requests per
// month and matches its hourly refresh.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          time.Hour,
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

// DefaultMarkets defines the built-in Open Exchange Rates fiat pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USD/KRW", Symbol: "USD/KRW"},
	{Pair: "USD/SDR", Symbol: "USD/XDR"},
	{Pair: "USD/CNY", Symbol: "USD/CNY"},
	{Pair: "USD/JPY", Symbol: "USD/JPY"},
	{Pair: "USD/EUR", Symbol: "USD/EUR"},
	{Pair: "USD/GBP", Symbol: "USD/GBP"},
	{Pair: "USD/MNT", Symbol: "USD/MNT"},
}
