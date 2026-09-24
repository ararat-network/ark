package coinbase

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: The endpoint is documented at
// https://docs.cdp.coinbase.com/coinbase-app/docs/api-prices#get-spot-price
// and needs no API key.

const (
	// Name is the name of the Coinbase API provider.
	Name = "coinbase_api"

	// URL is the Coinbase spot price endpoint. The product and the spot
	// segment are appended per request.
	URL = "https://api.coinbase.com/v2/prices"

	// SpotSegment is the path segment that selects the spot price.
	SpotSegment = "spot"

	// ProductSeparator separates base from quote in a product id.
	ProductSeparator = "-"
)

// DefaultAPIConfig is the default configuration for the Coinbase API. The
// endpoint prices one product per request, and the public v2 API allows
// 10,000 requests per hour per IP, so each ticker polls once a second.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          time.Second,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in Coinbase API pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDT-USD"},
}
