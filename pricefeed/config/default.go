package config

import (
	"time"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pricefeed/sidecar/chainstate"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/api/currencybeacon"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/api/openexchangerates"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	"github.com/ararat-network/ark/pricefeed/sidecar/runtime"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

const (
	// DefaultUpdateInterval is the default cadence for committing public price snapshots.
	DefaultUpdateInterval = 1500 * time.Millisecond

	// DefaultClientAddress is the default on-chain oracle query endpoint.
	DefaultClientAddress = "127.0.0.1:9090"
	// DefaultClientTimeout bounds each default feed query.
	DefaultClientTimeout = 2 * time.Second
	// DefaultClientInterval is the default feed polling cadence.
	DefaultClientInterval = 5 * time.Second

	// usdNOAH is the hub leg every default route ends on: NOAH per one
	// dollar, the orientation the chain stores.
	usdNOAH = "USD/NOAH"
)

// DefaultProviders defines the provider templates used by Default.
//
// CurrencyBeacon and Open Exchange Rates ship with placeholder API keys and
// fetch nothing until the operator replaces them.
var DefaultProviders = map[string]providers.Config{
	currencybeacon.Name: {
		Name:          currencybeacon.Name,
		TransportType: base.API,
		Markets:       currencybeacon.DefaultMarkets,
		MaxPriceAge:   currencybeacon.DefaultMaxPriceAge,
		API:           currencybeacon.DefaultAPIConfig,
	},
	frankfurter.Name: {
		Name:          frankfurter.Name,
		TransportType: base.API,
		Markets:       frankfurter.DefaultMarkets,
		MaxPriceAge:   frankfurter.DefaultMaxPriceAge,
		API:           frankfurter.DefaultAPIConfig,
	},
	openexchangerates.Name: {
		Name:          openexchangerates.Name,
		TransportType: base.API,
		Markets:       openexchangerates.DefaultMarkets,
		MaxPriceAge:   openexchangerates.DefaultMaxPriceAge,
		API:           openexchangerates.DefaultAPIConfig,
	},
}

// DefaultResolver defines the built-in USD-hub routes used by Default.
var DefaultResolver = resolver.Config{
	BootstrapPrices: []resolver.BootstrapPrice{
		{
			Pair:       usdNOAH,
			Price:      chain.BootstrapNoahPerUSD,
			ValidUntil: "2027-10-29T00:00:00Z",
		},
	},
	Routes: map[string][]resolver.Route{
		chain.USDBaseDenom: {
			{Name: "direct", Pairs: []sidecartypes.Pair{usdNOAH}},
		},
		chain.KRWBaseDenom: {
			{Name: "krw-usd-noah", Pairs: []sidecartypes.Pair{"KRW/USD", usdNOAH}},
		},
		chain.XDRBaseDenom: {
			{Name: "xdr-usd-noah", Pairs: []sidecartypes.Pair{"XDR/USD", usdNOAH}},
		},
		chain.CNYBaseDenom: {
			{Name: "cny-usd-noah", Pairs: []sidecartypes.Pair{"CNY/USD", usdNOAH}},
		},
		chain.JPYBaseDenom: {
			{Name: "jpy-usd-noah", Pairs: []sidecartypes.Pair{"JPY/USD", usdNOAH}},
		},
		chain.EURBaseDenom: {
			{Name: "eur-usd-noah", Pairs: []sidecartypes.Pair{"EUR/USD", usdNOAH}},
		},
		chain.GBPBaseDenom: {
			{Name: "gbp-usd-noah", Pairs: []sidecartypes.Pair{"GBP/USD", usdNOAH}},
		},
		chain.CADBaseDenom: {
			{Name: "cad-usd-noah", Pairs: []sidecartypes.Pair{"CAD/USD", usdNOAH}},
		},
		chain.AUDBaseDenom: {
			{Name: "aud-usd-noah", Pairs: []sidecartypes.Pair{"AUD/USD", usdNOAH}},
		},
		chain.SGDBaseDenom: {
			{Name: "sgd-usd-noah", Pairs: []sidecartypes.Pair{"SGD/USD", usdNOAH}},
		},
		chain.MXNBaseDenom: {
			{Name: "mxn-usd-noah", Pairs: []sidecartypes.Pair{"MXN/USD", usdNOAH}},
		},
	},
}

// Default returns the default sidecar runtime configuration.
func Default() runtime.Config {
	return runtime.Config{
		UpdateInterval: DefaultUpdateInterval,
		Providers:      DefaultProviders,
		Resolver:       DefaultResolver,
		Client: chainstate.Config{
			Address:  DefaultClientAddress,
			Timeout:  DefaultClientTimeout,
			Interval: DefaultClientInterval,
		},
		FallbackFeeds: []string{
			chain.USDBaseDenom,
			chain.KRWBaseDenom,
			chain.XDRBaseDenom,
			chain.CNYBaseDenom,
			chain.JPYBaseDenom,
			chain.EURBaseDenom,
			chain.GBPBaseDenom,
			chain.CADBaseDenom,
			chain.AUDBaseDenom,
			chain.SGDBaseDenom,
			chain.MXNBaseDenom,
		},
	}
}
