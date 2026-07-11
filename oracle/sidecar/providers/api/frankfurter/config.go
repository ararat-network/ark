package frankfurter

import (
	"time"

	"ark/oracle/sidecar/providers/base/api"
	"ark/oracle/sidecar/providers/types"
)

const (
	// Name is the name of the Frankfurter provider.
	Name = "frankfurter_api"

	// URL is the Frankfurter latest-rates endpoint.
	URL = "https://api.frankfurter.dev/v2/rates"

	// DefaultMaxPriceAge is the default freshness window for Frankfurter prices.
	DefaultMaxPriceAge = 90 * time.Second
)

// DefaultAPIConfig is the default configuration for the Frankfurter API.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          time.Minute,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in Frankfurter fiat pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USD/KRW", Symbol: "USD/KRW"},
	{Pair: "USD/SDR", Symbol: "USD/XDR"},
	{Pair: "USD/CNY", Symbol: "USD/CNY"},
	{Pair: "USD/JPY", Symbol: "USD/JPY"},
	{Pair: "USD/EUR", Symbol: "USD/EUR"},
	{Pair: "USD/GBP", Symbol: "USD/GBP"},
	{Pair: "USD/MNT", Symbol: "USD/MNT"},
}
