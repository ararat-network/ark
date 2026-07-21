package config

import (
	"time"

	"ark/oracle/sidecar/chainstate"
	"ark/oracle/sidecar/providers"
	"ark/oracle/sidecar/providers/api/frankfurter"
	"ark/oracle/sidecar/providers/base"
	"ark/oracle/sidecar/resolver"
	"ark/oracle/sidecar/runtime"
	sidecartypes "ark/oracle/sidecar/types"
	"ark/pkg/chain"
)

const (
	// DefaultUpdateInterval is the default cadence for committing public price snapshots.
	DefaultUpdateInterval = 1500 * time.Millisecond

	// DefaultClientAddress is the default on-chain oracle query endpoint.
	DefaultClientAddress = "127.0.0.1:9090"
	// DefaultClientTimeout bounds each default vote-target query.
	DefaultClientTimeout = 2 * time.Second
	// DefaultClientInterval is the default vote-target polling cadence.
	DefaultClientInterval = 5 * time.Second
)

// DefaultProviders defines the provider templates used by Default.
var DefaultProviders = map[string]providers.Config{
	frankfurter.Name: {
		Name:          frankfurter.Name,
		TransportType: base.API,
		Markets:       frankfurter.DefaultMarkets,
		MaxPriceAge:   frankfurter.DefaultMaxPriceAge,
		API:           frankfurter.DefaultAPIConfig,
	},
}

// DefaultResolver defines the built-in USD-hub routes used by Default.
var DefaultResolver = resolver.Config{
	BootstrapPrices: []resolver.BootstrapPrice{
		{
			Pair:       "NOAH/USD",
			Price:      "1",
			ValidUntil: "2027-10-29T00:00:00Z",
		},
	},
	Routes: map[string][]resolver.Route{
		chain.MicroUSDDenom: {
			{Name: "direct", Pairs: []sidecartypes.Pair{"NOAH/USD"}},
		},
		chain.MicroKRWDenom: {
			{Name: "noah-usd-krw", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/KRW"}},
		},
		chain.MicroSDRDenom: {
			{Name: "noah-usd-sdr", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/SDR"}},
		},
		chain.MicroCNYDenom: {
			{Name: "noah-usd-cny", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/CNY"}},
		},
		chain.MicroJPYDenom: {
			{Name: "noah-usd-jpy", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/JPY"}},
		},
		chain.MicroEURDenom: {
			{Name: "noah-usd-eur", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/EUR"}},
		},
		chain.MicroGBPDenom: {
			{Name: "noah-usd-gbp", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/GBP"}},
		},
		chain.MicroMNTDenom: {
			{Name: "noah-usd-mnt", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/MNT"}},
		},
	},
}

// Default returns the default standalone oracle runtime configuration.
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
		FallbackDenoms: []string{
			chain.MicroUSDDenom,
			chain.MicroKRWDenom,
			chain.MicroSDRDenom,
			chain.MicroCNYDenom,
			chain.MicroJPYDenom,
			chain.MicroEURDenom,
			chain.MicroGBPDenom,
			chain.MicroMNTDenom,
		},
	}
}
