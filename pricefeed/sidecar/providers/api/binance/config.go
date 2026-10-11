package binance

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: All documentation for this file can be located on the Binance GitHub
// API documentation: https://github.com/binance/binance-spot-api-docs/blob/master/rest-api.md#symbol-price-ticker. This
// API does not require a subscription to use (i.e. No API key is required).

const (
	// Name is the name of the Binance provider.
	Name = "binance_api"

	// URL is the Binance ticker price endpoint for non-US users. The
	// requested symbols go in the symbols query parameter, so an override such
	// as Binance.US's endpoint is a plain URL too.
	URL = "https://api.binance.com/api/v3/ticker/price"

	// SymbolsParameter carries the requested symbols as a JSON array.
	SymbolsParameter = "symbols"
)

// DefaultNonUSAPIConfig is the default configuration for the Binance API.
var DefaultNonUSAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          750 * time.Millisecond,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
}

// DefaultMarkets defines the built-in Binance API pair mappings.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDTUSD"},
}
