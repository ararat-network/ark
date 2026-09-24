package bitstamp

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: The endpoint is documented at https://www.bitstamp.net/api/#tag/Tickers
// and needs no API key.

const (
	// Name is the name of the Bitstamp API provider.
	Name = "bitstamp_api"

	// URL is the Bitstamp ticker endpoint. It returns every market in one
	// response, so the request carries no ticker.
	URL = "https://www.bitstamp.net/api/v2/ticker/"
)

// DefaultAPIConfig is the default configuration for the Bitstamp API.
var DefaultAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          3000 * time.Millisecond,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in Bitstamp API pair mappings. The symbol is
// the pair field of the ticker response.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "USDT/USD"},
}
