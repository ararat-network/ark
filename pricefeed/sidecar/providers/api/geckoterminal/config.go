package geckoterminal

import (
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// NOTE: The endpoint is documented at
// https://apiguide.geckoterminal.com/getting-started and needs no API key.

const (
	// Name is the name of the GeckoTerminal API provider.
	Name = "gecko_terminal_api"

	// ETHURL is the token price endpoint for Ethereum mainnet. The network is
	// part of the path, so every ticker on one provider shares a network; the
	// token addresses are appended per request.
	ETHURL = "https://api.geckoterminal.com/api/v2/simple/networks/eth/token_price"

	// ExpectedResponseType is the data type the endpoint returns.
	ExpectedResponseType = "simple_token_price"

	// MaxAddressesPerRequest is the number of token addresses the endpoint
	// prices in one request.
	MaxAddressesPerRequest = 30
)

// DefaultETHAPIConfig is the default configuration for pricing Ethereum
// mainnet tokens on the GeckoTerminal API. The public rate limit is low, so
// polling is slow.
var DefaultETHAPIConfig = api.Config{
	Name:              Name,
	Timeout:           3000 * time.Millisecond,
	Interval:          20 * time.Second,
	RequestsPerSecond: 0,
	Endpoints:         []types.Endpoint{{URL: ETHURL}},
	BatchSize:         MaxAddressesPerRequest,
	MaxBlockHeightAge: 0,
}

// DefaultMarkets defines the built-in GeckoTerminal pair mappings for
// Ethereum mainnet. The symbol is the token contract address; prices are in
// USD.
var DefaultMarkets = types.Markets{
	{Pair: "USDT/USD", Symbol: "0xdac17f958d2ee523a2206206994597c13d831ec7"},
}
