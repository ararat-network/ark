package binance

import (
	"time"

	"noah/oracle/providers/base/api"
	"noah/oracle/providers/types"
)

// NOTE: All documentation for this file can be located on the Binance GitHub
// API documentation: https://github.com/binance/binance-spot-api-docs/blob/master/rest-api.md#symbol-price-ticker. This
// API does not require a subscription to use (i.e. No API key is required).

const (
	// Name is the name of the Binance provider.
	Name = "binance_api"

	// URL is the base URL of the Binance API. This includes the base and quote
	// currency pairs that need to be inserted into the URL. This URL should be utilised
	// by Non-US users.
	URL = "https://api.binance.com/api/v3/ticker/price?symbols=%s%s%s"

	Quotation    = "%22"
	Separator    = ","
	LeftBracket  = "%5B"
	RightBracket = "%5D"
)

// DefaultNonUSAPIConfig is the default configuration for the Binance API.
var DefaultNonUSAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          750 * time.Millisecond,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: URL}},
	BatchSize:         0,
	MaxBlockHeightAge: 30 * time.Second,
}

var DefaultMarkets = types.Markets{
	{Denom: "uusd", Symbol: "USDTUSD"},
}
