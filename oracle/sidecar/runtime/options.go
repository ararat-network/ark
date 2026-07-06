package runtime

import (
	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers/base"
)

// Option customises oracle construction.
type Option func(impl *Runtime)

// WithLogger sets the logger for the oracle.
func WithLogger(logger log.Logger) Option {
	return func(r *Runtime) {
		r.logger = logger
	}
}

// WithProviders allows pre-instantiated price providers to be used in the Runtime's price fetching loop.
// This option is mainly used for testing, but can be useful for programmatically setting customised providers.
func WithProviders(ps ...*base.Provider) Option {
	return func(r *Runtime) {
		for _, p := range ps {
			r.providers[p.Name()] = p
		}
	}
}

// WithChainStateClient sets the client used to fetch on-chain oracle vote targets.
func WithChainStateClient(client ChainStateClient) Option {
	return func(r *Runtime) {
		r.client = client
	}
}

// WithResolver sets the price resolver used by the oracle.
func WithResolver(resolver PriceResolver) Option {
	return func(r *Runtime) {
		r.resolver = resolver
	}
}
