package config

import (
	"time"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pricefeed/sidecar/chainstate"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
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
		chain.USDBaseDenom: {
			{Name: "direct", Pairs: []sidecartypes.Pair{"NOAH/USD"}},
		},
		chain.KRWBaseDenom: {
			{Name: "noah-usd-krw", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/KRW"}},
		},
		chain.SDRBaseDenom: {
			{Name: "noah-usd-sdr", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/SDR"}},
		},
		chain.CNYBaseDenom: {
			{Name: "noah-usd-cny", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/CNY"}},
		},
		chain.JPYBaseDenom: {
			{Name: "noah-usd-jpy", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/JPY"}},
		},
		chain.EURBaseDenom: {
			{Name: "noah-usd-eur", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/EUR"}},
		},
		chain.GBPBaseDenom: {
			{Name: "noah-usd-gbp", Pairs: []sidecartypes.Pair{"NOAH/USD", "USD/GBP"}},
		},
		chain.MNTBaseDenom: {
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
		FallbackFeeds: []string{
			chain.USDBaseDenom,
			chain.KRWBaseDenom,
			chain.SDRBaseDenom,
			chain.CNYBaseDenom,
			chain.JPYBaseDenom,
			chain.EURBaseDenom,
			chain.GBPBaseDenom,
			chain.MNTBaseDenom,
		},
	}
}
