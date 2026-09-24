package polymarket

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: The endpoint is documented at
// https://docs.polymarket.com/developers/CLOB/markets/get-market and needs no
// API key.

const (
	// Name is the name of the Polymarket API provider.
	Name = "polymarket_api"

	// URL is the Polymarket CLOB markets endpoint. The market's condition id
	// is appended per request.
	URL = "https://clob.polymarket.com/markets"

	// TickerSeparator separates the condition id from the token id in a
	// provider symbol.
	TickerSeparator = "/"

	// MinPrice replaces a zero outcome price. A zero would be dropped by the
	// resolver, which needs a positive price; the floor keeps a settled
	// outcome observable.
	MinPrice = "0.0001"
)

// DefaultAPIConfig is the default configuration for the Polymarket API.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          500 * time.Millisecond,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}
